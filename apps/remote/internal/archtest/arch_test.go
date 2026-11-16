// Package archtest 用测试来固定包依赖方向。
//
// 为什么写成测试而不是 lint 配置：不引第三方工具、不增加依赖，
// `go test ./...` 跑到它就已经是一条门禁；越界时给出文件名和具体 import。
//
// 规则来源：docs/architecture/rules.md。修改分层时应同时改这里和那份文档。
package archtest

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const internalPrefix = "mobilevc/internal/"

// allowed 声明每个内部包允许依赖的内部包（按前缀匹配）。
// nil 表示叶子包：不允许 import 任何 internal 包。
// 不在表里的包视为组合根（cmd/*、gateway、relayclient），允许任意依赖。
var allowed = map[string][]string{
	"protocol": nil,
	"logx":     nil,
	"push":     nil,
	"adb":      nil,

	"data":    {"protocol"},
	"engine":  {"logx", "protocol"},
	"relay":   {"logx"},
	"session": {"adb", "data", "engine", "logx", "protocol"},
}

func TestImportBoundaries(t *testing.T) {
	root := filepath.Join("..", "..") // apps/remote

	for pkg, allow := range allowed {
		pkg := pkg
		allow := allow
		t.Run(pkg, func(t *testing.T) {
			dir := filepath.Join(root, "internal", pkg)
			err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
					return nil
				}
				file, parseErr := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
				if parseErr != nil {
					return parseErr
				}
				for _, spec := range file.Imports {
					importPath, unquoteErr := strconv.Unquote(spec.Path.Value)
					if unquoteErr != nil || !strings.HasPrefix(importPath, internalPrefix) {
						continue
					}
					if isAllowed(importPath, pkg, allow) {
						continue
					}
					rel, _ := filepath.Rel(root, path)
					t.Errorf("%s: 不允许 import %q（%s 的允许清单：%v）", rel, importPath, pkg, allow)
				}
				return nil
			})
			if err != nil {
				t.Fatalf("walk %s: %v", pkg, err)
			}
		})
	}
}

// isAllowed 按前缀判断：允许 "data" 也就允许 "data/claudesync" 这类子包。
// 自身及子包（父包与子包互相引用）永远允许，不属于跨层依赖。
func isAllowed(importPath, pkg string, allow []string) bool {
	rest := strings.TrimPrefix(importPath, internalPrefix)
	if rest == pkg || strings.HasPrefix(rest, pkg+"/") {
		return true
	}
	for _, name := range allow {
		if rest == name || strings.HasPrefix(rest, name+"/") {
			return true
		}
	}
	return false
}
