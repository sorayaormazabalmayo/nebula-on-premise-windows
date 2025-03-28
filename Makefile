.PHONY: release build
release:
	.\.scripts\make-release.bat
build:
	echo "Building ..."
	.\.scripts\make-build.bat
