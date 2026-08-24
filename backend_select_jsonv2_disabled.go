//go:build sonic_jsonv2 && !sonic_stdjson && !goexperiment.jsonv2

package sonic

import (
	"github.com/bytedance/sonic/ast"
	"github.com/bytedance/sonic/internal/backend"
)

var _ = sonicJSONV2RequiresGOEXPERIMENTJSONV2

func newBackend(backend.Config) backend.Backend { return nil }

func selectedGet([]byte, ast.SearchOptions, ...interface{}) (ast.Node, error) { return ast.Node{}, nil }
