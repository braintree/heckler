build: vendor/github.com/libgit2/git2go/static-build
	go build -o . -mod=vendor -tags=static ./...

vendor/github.com/libgit2/git2go/static-build:
	./build-libgit2-static

.PHONY: deb
deb:
	debuild -us -uc -b

.PHONY: clean
clean:
	rm -rf vendor/github.com/libgit2/git2go/static-build
	rm -rf vendor/github.com/libgit2/git2go/script
	rm -rf vendor/github.com/libgit2/git2go/vendor
	rm -rf _build
