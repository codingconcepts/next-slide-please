validate_version:
ifndef VERSION
	$(error VERSION is undefined)
endif

release: validate_version
	# macos
	GOOS=darwin go build -ldflags "-X main.version=${VERSION}" -o nsp cmd/nsp/nsp.go;\
	tar -zcvf ./releases/dg_${VERSION}_macOS.tar.gz ./nsp ;\

	rm ./nsp

dev_install: validate_version
	tar -xvf ./releases/dg_${VERSION}_macOS.tar.gz
	mv nsp ~/dev/bin