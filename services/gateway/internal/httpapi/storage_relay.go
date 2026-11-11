package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/sgao19/erp-go/pkg/blobstore"
)

// 文件中转：浏览器连不上对象存储时，由网关替它收发。
//
// 系统里的附件一直是「预签名直传」：服务签一个十分钟有效的地址，浏览器拿着
// 它直接把文件 PUT 进对象存储，文件字节不经过网关。这在美国和南美一直好好的。
// 2026-11-09 天津办公室的 F12 给出了另一面：浏览器连签名地址里那台主机，
// `net::ERR_CONNECTION_TIMED_OUT`；而到本站的连接他们每天都在用、一直是通的。
// 桶里从上线到那天，国内员工直传成功的次数是 0（质检资料、报销凭证各 0 份），
// 走网关中转的供应商资料和询价附件却都传上去了。
//
// 所以多一条路：页面照旧先向业务服务要签名地址，然后不自己去存储，而是把
// 文件连同那个地址交给网关；网关连存储、把字节送过去、把存储的回答带回来。
// 读文件同理。业务服务不用改一行——它们看到的还是「签地址 → 登记」两步。
//
// 中转不是代理：它只认本系统自己那个桶的签名地址。地址来自哪台主机、哪个
// 桶、带没带签名，都在 target 里查；查不过的一律不连。签名本身仍由存储验证，
// 网关不持有也不需要存储的密钥——它转发的权限就是那个地址里的签名。
//
// **「自己的地址长什么样」不是按配置拼的，是问签名的那个库要来的**
// （blobstore.PresignedShapes）。第一版是拼的：`<桶>.<MINIO_PUBLIC_ENDPOINT>`，
// 看着理所当然。而对着 AWS，签名库签出来的主机是
// `<桶>.s3.dualstack.<区域>.amazonaws.com`——多一段 dualstack，区域也在里面。
// 于是 2026-11-09 上线到 11-11，生产上 27 次上传全部被这里当成「不是本系统的
// 文件存储」挡了回去，一次都没走到存储；而测试全绿，因为测试里的「生产地址」
// 是照同一个想当然写的。现在测试里的生产地址由真的签名代码签（NewOffline），
// 认地址的规则和它出自同一处。
//
// 网关这一步文件只在内存里过：PUT 的字节边收边转，GET 的字节边读边写。前门
// nginx 收请求体时会先落一个临时文件，请求结束即删（它的默认行为，不归这里管）。

// StorageURLHeader 是页面把签名地址交给中转时用的请求头。放头里而不是 query：
// query 会原样进 nginx 的 access log，签名也就跟着进去了。
const StorageURLHeader = "X-Storage-Url"

// storageRelayMaxBytes 对齐前门 nginx 的 client_max_body_size（64m）。
const storageRelayMaxBytes int64 = 64 << 20

// storageRelayUploadTimeout 是网关把一份文件送进存储的上限。网关到存储在同一
// 个区域，64 MB 几秒钟的事；这个值只防一个挂死的连接把 goroutine 永远占住。
const storageRelayUploadTimeout = 10 * time.Minute

// StorageRelay 知道怎么从一个签名地址认出自己的存储，以及实际该连到哪儿。
type StorageRelay struct {
	// signed 是业务服务签给浏览器的地址的样子：协议、主机、键前面那段路径。
	signed blobstore.URLShape
	// dialHost 非空时，实际连接换成这台主机——开发环境签的是 localhost:19000，
	// 网关在容器里够得着的是 minio:9000。签名盖住了 Host，所以连谁可以换，
	// 发出去的 Host 头必须还是签名时那个。生产上两边是同一台，这里为空。
	dialHost string
	client   *http.Client
	maxBytes int64
}

// NewStorageRelay 按存储配置建中转。cfg 必须和签地址的业务服务用同一组值
// （Endpoint、PublicEndpoint、Bucket、UseSSL、Region；密钥用不着）——所以网关
// 读的是和它们同名的 MINIO_* 变量，一份 deploy/.env 管所有人。区域尤其不能
// 漏：对着 AWS 它是主机名的一部分，漏了认的就是另一个区域的地址。
func NewStorageRelay(cfg blobstore.Config) (*StorageRelay, error) {
	signed, internal, err := blobstore.PresignedShapes(cfg)
	if err != nil {
		return nil, err
	}
	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		transport = &http.Transport{}
	}
	transport = transport.Clone()
	transport.ResponseHeaderTimeout = 60 * time.Second
	sr := &StorageRelay{
		signed:   signed,
		maxBytes: storageRelayMaxBytes,
		client: &http.Client{
			Transport: transport,
			// 存储答 3xx（比如区域不对的 301）就是出了错，不替它跟过去。
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
	// 只换主机。协议和路径都签过名、换不得；两边除主机外还有别的不一样时，
	// 就按签名的地址原样去连（够不着会答 502，原因写在日志里）。
	if !signed.SameHost(internal.Host) &&
		internal.Scheme == signed.Scheme && internal.PathPrefix == signed.PathPrefix {
		sr.dialHost = internal.Host
	}
	return sr, nil
}

// LogAttrs 是启动时记一行用的：网关认哪个地址、实际连哪台。上线后拿这一行
// 核对，比等人点一次上传再看报错快得多。
func (sr *StorageRelay) LogAttrs() []any {
	dial := sr.dialHost
	if dial == "" {
		dial = sr.signed.Host
	}
	return []any{
		"signed_host", sr.signed.Host, "dial_host", dial,
		"scheme", sr.signed.Scheme, "path_prefix", sr.signed.PathPrefix,
	}
}

// target 核对 raw 是不是本系统自己那个桶的签名地址，是的话给出实际要连的地址
// 和要带的 Host 头。路径和查询串原样保留——签名就是按它们算的，动一个字节
// 存储就会拒绝。
func (sr *StorageRelay) target(raw string) (dial *url.URL, host string, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, "", errors.New("缺少文件地址")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, "", errors.New("不是一个完整的地址")
	}
	if u.Scheme != sr.signed.Scheme {
		return nil, "", fmt.Errorf("地址应以 %s:// 开头", sr.signed.Scheme)
	}
	if u.User != nil {
		return nil, "", errors.New("地址不能带用户名")
	}
	// 键是我们自己拼的（文件名过了 filepath.Base），不会有这些；出现就是有人在试。
	// u.Path 是解码后的，所以 %2e%2e 也落在这里。
	if strings.Contains(u.Path, "/../") || strings.HasSuffix(u.Path, "/..") ||
		strings.Contains(u.Path, "/./") || strings.HasSuffix(u.Path, "/.") || strings.Contains(u.Path, "//") {
		return nil, "", errors.New("地址里的路径不正常")
	}
	if !sr.signed.SameHost(u.Host) {
		return nil, "", errors.New("不是本系统的文件存储")
	}
	// 主机对了，再看键前面那段：路径式的存储，桶名在路径里。
	if !sr.signed.Covers(u) {
		return nil, "", errors.New("不是本系统的存储桶")
	}
	q := u.Query()
	if q.Get("X-Amz-Signature") == "" || q.Get("X-Amz-Credential") == "" {
		return nil, "", errors.New("地址没有签名")
	}

	dialURL := *u
	if sr.dialHost != "" {
		dialURL.Host = sr.dialHost
	}
	return &dialURL, u.Host, nil
}

// offeredHost 只取主机，给日志用：整条地址里有签名，不能记。
func offeredHost(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	if len(u.Host) > 255 {
		return u.Host[:255]
	}
	return u.Host
}

func (s *Server) storageRelayReady(w http.ResponseWriter, r *http.Request) (dial *url.URL, host string, ok bool) {
	if s.Storage == nil {
		s.writeError(w, http.StatusServiceUnavailable, "STORAGE_RELAY_UNAVAILABLE", "文件中转未配置")
		return nil, "", false
	}
	raw := r.Header.Get(StorageURLHeader)
	dial, host, err := s.Storage.target(raw)
	if err != nil {
		// 记下来的是「收到哪台主机、本该是哪台」。第一版没记，结果生产上认错主机
		// 两天，日志里只有一串 400；有这一行，对一眼就知道是谁和谁没对上。
		if s.Log != nil {
			s.Log.Warn("storage relay refused an address", "reason", err.Error(),
				"offered_host", offeredHost(raw), "signed_host", s.Storage.signed.Host)
		}
		s.writeError(w, http.StatusBadRequest, "STORAGE_RELAY_URL_REJECTED", "文件地址无效："+err.Error())
		return nil, "", false
	}
	return dial, host, true
}

// relayUpload: PUT /api/storage/relay，请求体是文件本身，X-Storage-Url 是签名地址。
func (s *Server) relayUpload(w http.ResponseWriter, r *http.Request) {
	dial, host, ok := s.storageRelayReady(w, r)
	if !ok {
		return
	}
	// 签名 PUT 不接受分块传输，没有长度就送不出去；浏览器传 File 一定带长度，
	// 前门 nginx 也会先收完整个请求体再带着长度转过来。
	if r.ContentLength < 0 {
		s.writeError(w, http.StatusLengthRequired, "STORAGE_RELAY_LENGTH_REQUIRED", "缺少文件大小")
		return
	}
	if r.ContentLength > s.Storage.maxBytes {
		s.writeError(w, http.StatusRequestEntityTooLarge, "STORAGE_RELAY_TOO_LARGE",
			fmt.Sprintf("文件不能超过 %d MB", s.Storage.maxBytes>>20))
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), storageRelayUploadTimeout)
	defer cancel()
	// 空文件要明说长度是 0：Body 非空而 ContentLength 为 0，Go 的客户端会当成
	// 「不知道多长」改用分块，存储就拒收了。
	var body io.Reader = http.NoBody
	if r.ContentLength > 0 {
		body = http.MaxBytesReader(w, r.Body, s.Storage.maxBytes)
	}
	up, err := http.NewRequestWithContext(ctx, http.MethodPut, dial.String(), body)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "STORAGE_RELAY_URL_REJECTED", "文件地址无效")
		return
	}
	up.ContentLength = r.ContentLength
	up.Host = host
	contentType := r.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	up.Header.Set("Content-Type", contentType)

	resp, err := s.Storage.client.Do(up)
	if err != nil {
		// 浏览器自己断了（关页面、取消）：没人等答案，也不是存储的错。
		if r.Context().Err() != nil {
			return
		}
		s.logStorageRelay("storage relay upload failed", dial, 0, "", err)
		s.writeError(w, http.StatusBadGateway, "STORAGE_RELAY_UNREACHABLE", "服务器连不上文件存储，请稍后再试")
		return
	}
	defer resp.Body.Close()
	// 存储的错误正文是一小段 XML，<Code> 才是真正的原因（SignatureDoesNotMatch、
	// AccessDenied……），带给人看；成功时正文为空。读掉它也让连接能复用。
	answer, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode/100 != 2 {
		code := storeErrorCode(answer)
		s.logStorageRelay("storage relay upload refused", dial, resp.StatusCode, code, nil)
		s.writeError(w, http.StatusBadGateway, "STORAGE_RELAY_REJECTED", storeRefusal("上传", resp.StatusCode, code))
		return
	}
	s.writeStorageRelayResult(w, map[string]any{
		"etag": strings.Trim(resp.Header.Get("ETag"), `"`),
		"size": r.ContentLength,
	})
}

// relayDownload: GET /api/storage/relay，X-Storage-Url 是签名的读取地址。
func (s *Server) relayDownload(w http.ResponseWriter, r *http.Request) {
	dial, host, ok := s.storageRelayReady(w, r)
	if !ok {
		return
	}
	up, err := http.NewRequestWithContext(r.Context(), http.MethodGet, dial.String(), nil)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "STORAGE_RELAY_URL_REJECTED", "文件地址无效")
		return
	}
	up.Host = host
	resp, err := s.Storage.client.Do(up)
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		s.logStorageRelay("storage relay download failed", dial, 0, "", err)
		s.writeError(w, http.StatusBadGateway, "STORAGE_RELAY_UNREACHABLE", "服务器连不上文件存储，请稍后再试")
		return
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		s.writeError(w, http.StatusNotFound, "STORAGE_RELAY_NOT_FOUND", "文件不存在或已被删除")
		return
	case resp.StatusCode/100 != 2:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		code := storeErrorCode(body)
		s.logStorageRelay("storage relay download refused", dial, resp.StatusCode, code, nil)
		s.writeError(w, http.StatusBadGateway, "STORAGE_RELAY_REJECTED", storeRefusal("读取", resp.StatusCode, code))
		return
	}
	for _, h := range []string{"Content-Type", "Content-Length", "Last-Modified", "ETag"} {
		if v := resp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	// 从本站域名吐出来的用户文件一律当下载：谁上传一个 HTML，别人点开时不能
	// 在我们的域名下当页面跑。文件名沿用存储给的（PresignedGetAs 签进去的那
	// 个），没有就只标 attachment，页面自己知道该叫什么。
	w.Header().Set("Content-Disposition", relayDisposition(resp.Header.Get("Content-Disposition")))
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, resp.Body)
}

// relayDisposition 把存储给的 Content-Disposition 改成 attachment。存储给了文件名
// （PresignedGetAs 签进去的那个）就沿用，两种写法由 attachmentDisposition 重新
// 生成；没给或解析不了就只剩 attachment，页面自己知道该叫什么。
func relayDisposition(upstream string) string {
	if upstream == "" {
		return "attachment"
	}
	_, params, err := mime.ParseMediaType(upstream)
	if err != nil || params["filename"] == "" {
		return "attachment"
	}
	return attachmentDisposition(params["filename"])
}

// storeErrorCode 从存储的错误正文里取 <Code>——S3 和 MinIO 的错误都是这个形状。
var storeErrorCodePattern = regexp.MustCompile(`<Code>([A-Za-z0-9._-]{1,64})</Code>`)

func storeErrorCode(body []byte) string {
	if m := storeErrorCodePattern.FindSubmatch(body); m != nil {
		return string(m[1])
	}
	return ""
}

// storeRefusal 是给人看的那句话：状态码之外把存储自己的说法也带上，
// 「签名过期」和「密钥不对」才分得开。
func storeRefusal(action string, status int, code string) string {
	if code != "" {
		return fmt.Sprintf("文件存储拒绝了这次%s（HTTP %d %s）", action, status, code)
	}
	return fmt.Sprintf("文件存储拒绝了这次%s（HTTP %d）", action, status)
}

func (s *Server) logStorageRelay(msg string, dial *url.URL, status int, code string, err error) {
	if s.Log == nil {
		return
	}
	// 地址只记主机和路径，不记查询串——查询串里就是签名。*url.Error 的文本里
	// 带着完整地址（连同签名），所以只留它里面那层真正的原因。
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	s.Log.Warn(msg, "host", dial.Host, "path", dial.Path, "status", status, "code", code, "err", err)
}

func (s *Server) writeStorageRelayResult(w http.ResponseWriter, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "GATEWAY_MARSHAL", "响应编码失败")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(envelope{Success: true, Data: data})
}
