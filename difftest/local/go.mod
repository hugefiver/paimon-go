module sonic-difftest-local

go 1.24

require github.com/bytedance/sonic v1.15.2

require github.com/valyala/fastjson v1.6.10 // indirect

replace github.com/bytedance/sonic => ../..
