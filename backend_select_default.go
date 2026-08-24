//go:build !sonic_jsonv2

package sonic

import (
	"github.com/bytedance/sonic/ast"
	"github.com/bytedance/sonic/internal/backend"
	"github.com/bytedance/sonic/internal/fastjsoncompat"
)

func newBackend(_ backend.Config) backend.Backend {
	return defaultBackend{}
}

func selectedGet(data []byte, opts ast.SearchOptions, path ...interface{}) (ast.Node, error) {
	return fastjsoncompat.Get(data, opts, path...)
}
