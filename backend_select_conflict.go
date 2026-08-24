//go:build sonic_jsonv2 && sonic_stdjson

package sonic

import (
	"github.com/bytedance/sonic/ast"
	"github.com/bytedance/sonic/internal/backend"
)

var _ = sonicBuildTagsSonicStdJSONAndSonicJSONV2AreMutuallyExclusive

func newBackend(backend.Config) backend.Backend { return nil }

func selectedGet([]byte, ast.SearchOptions, ...interface{}) (ast.Node, error) { return ast.Node{}, nil }
