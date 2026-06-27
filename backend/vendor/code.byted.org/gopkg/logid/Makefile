set_env=GOMODPROXY="https://go-mod-proxy.byted.org,https://proxy.golang.org,direct" GOPRIVATE="*.byted.org,*.everphoto.cn"
local_package=code.byted.org/gopkg/logid

.PHONY: lint
.PHONY: test

run:
	make lint
	make test

lint:
	@$(set_env) go fmt ./...
	@$(set_env) go vet ./...
	@$(set_env) goimports -local $(local_package) -w .
	@$(set_env) go mod tidy

test:
	$(set_env) test -z "$$(gofmt -s -l .)"
	$(set_env) test -z "$$(goimports -local $(local_package) -d .)"
	./.codebase/test.sh

html:
	go tool cover -html=c.out
