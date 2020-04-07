GIT_COMMIT=$(shell git rev-list -1 HEAD)
all:
	go build -ldflags "-X main.GitCommit=$(GIT_COMMIT)"
