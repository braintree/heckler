build: vendor/github.com/libgit2/git2go/static-build
	go build -o . -mod=vendor -tags=static ./...

vendor/github.com/libgit2/git2go/static-build:
	./build-libgit2-static

.PHONY: deb
deb:
	dpkg-buildpackage -us -uc -b -nc

.PHONY: clean
clean:
	rm -rf vendor/github.com/libgit2/git2go/static-build
	rm -rf vendor/github.com/libgit2/git2go/script
	rm -rf vendor/github.com/libgit2/git2go/vendor
	./debian/rules clean
