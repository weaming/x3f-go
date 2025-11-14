build-for-dev: fmt
	fish build.fish

build-all:
	fish build-all.fish

fmt:
	go fmt ./...