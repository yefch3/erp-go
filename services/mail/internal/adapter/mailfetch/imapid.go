package mailfetch

import (
	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
)

// IMAP ID（RFC 2971）：登录之后向服务器自报家门。
//
// **为什么非发不可。** 网易系（163 / 126 / yeah.net / 188）会拒绝没有自报
// 家门的客户端，而且不是在登录那一步拒绝，是在**打开信箱**那一步：
//
//	打开 INBOX 失败：EXAMINE Unsafe Login. Please contact kefu@188.com for help.
//
// 登录明明成功了，授权码也是对的，所以从错误上完全看不出该去补一条 ID 命令。
// 客户绑完 163 之后一封信都收不到，就是这个。
//
// 实测（TestHostCapabilities）163、126、QQ、263 四家**都**声明支持 ID，所以这
// 条命令实际上是发给所有人的——真实的邮件客户端也是这么做的，RFC 2971 里它
// 就是一条纯粹的自我介绍。先问一句 Support 不是为了区分服务商，是为了那些
// 压根没有这个扩展的服务器（自建的居多）：对它们发一条不认识的命令，只会白
// 换来一个 BAD。
const idCapability = "ID"

// idFields 是我们报上去的身份。诚实填写：网易要的就是「你是谁」。
var idFields = []string{
	"name", "erp-go",
	"version", "1.0",
	"vendor", "erp-go",
}

// idCommand 是 ID 命令本身。go-imap v1 没有这个扩展，而为一条命令引一个依赖
// 不值得——它就是一个命令名加一串键值。
type idCommand struct {
	fields []string
}

func (c *idCommand) Command() *imap.Command {
	list := make([]interface{}, 0, len(c.fields))
	for _, f := range c.fields {
		list = append(list, f)
	}
	return &imap.Command{
		Name: idCapability,
		// 一层列表：ID ("name" "erp-go" "version" "1.0" ...)
		Arguments: []interface{}{list},
	}
}

// announceID 发一次 ID。
//
// **失败一律不往上抛。** 这是一条自我介绍，不是一次认证：服务器不认它、答了
// BAD、或者干脆没回，都不该让一个本来能用的信箱绑不上或者收不了信。真正需要
// 它的服务商（网易）会正常回 OK；不需要的服务商压根不会走到这里，因为下面
// 先问过它支不支持。
func announceID(c *client.Client, log logger) {
	ok, err := c.Support(idCapability)
	if err != nil || !ok {
		return
	}
	// 处理器传 nil：服务器会回一条 * ID (...) 说它自己是谁，而我们不关心。
	// 试过了——不收下它既不报错也不写日志，连接照常可用，所以专门写一个把它
	// 吞掉的处理器纯属多余。
	status, err := c.Execute(&idCommand{fields: idFields}, nil)
	if err == nil && status != nil {
		err = status.Err()
	}
	if err != nil && log != nil {
		log.Warn("IMAP ID was refused; a NetEase mailbox may report Unsafe Login", "err", err)
	}
}

// logger is the sliver of *slog.Logger this file needs, so the behaviour can be
// tested without one.
type logger interface {
	Warn(msg string, args ...any)
}
