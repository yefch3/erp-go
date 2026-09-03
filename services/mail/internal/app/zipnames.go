package app

import (
	"fmt"
	"path"
	"strings"
)

// zipEntryNames 决定打包时每个附件在压缩包里叫什么。
//
// 两件事必须在这里做完，因为两件都会**静默**出错：
//
//   - **重名。** 同一封信里两个附件叫同一个名字是常事（生产上 186 封信如此，
//     报价单来回改几版就会这样）。zip 允许重名条目，但解压时后一个覆盖前一个，
//     于是十个附件解出来只有八个，而且没有任何报错。加序号，不合并。
//   - **名字来自发件人。** "../../etc/passwd" 或 "C:\x\a.doc" 原样写进条目名，
//     解压软件可能就照着写到那个位置去。只取最后一段，去掉路径分隔符。
//
// 顺序和传入一致：压缩包里的顺序应该和页面上看到的一致。
func zipEntryNames(fileNames []string) []string {
	out := make([]string, 0, len(fileNames))
	used := map[string]bool{}
	for i, raw := range fileNames {
		name := safeEntryName(raw, i)
		base, ext := splitExt(name)
		candidate := name
		for n := 2; used[strings.ToLower(candidate)]; n++ {
			candidate = fmt.Sprintf("%s (%d)%s", base, n, ext)
		}
		used[strings.ToLower(candidate)] = true
		out = append(out, candidate)
	}
	return out
}

// safeEntryName 把发件人给的文件名压成一个能安全写进压缩包的名字。
func safeEntryName(raw string, index int) string {
	name := strings.TrimSpace(raw)
	// 反斜杠也是分隔符：Windows 客户端发来的名字里就有。path.Base 只认斜杠。
	name = strings.ReplaceAll(name, "\\", "/")
	name = path.Base(name)
	name = strings.Trim(name, " .")
	// 控制字符不该出现在文件名里，去掉而不是替换——它们本来就不表达什么。
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, name)
	if name == "" || name == "." || name == ".." {
		return fmt.Sprintf("附件-%d", index+1)
	}
	return name
}

// splitExt 把 "报价 v2.final.xlsx" 拆成 ("报价 v2.final", ".xlsx")，好把序号
// 加在扩展名前面——"报价 (2).xlsx" 双击能打开，"报价.xlsx (2)" 不能。
func splitExt(name string) (base, ext string) {
	i := strings.LastIndexByte(name, '.')
	if i <= 0 {
		return name, ""
	}
	return name[:i], name[i:]
}
