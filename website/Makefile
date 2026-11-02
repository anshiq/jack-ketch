.PHONY: dev build clean sync-docs check

HUGO ?= hugo

sync-docs:
	@sh scripts/sync-docs.sh

dev: sync-docs
	@$(HUGO) server --disableFastRender --navigateToChanged

build: sync-docs
	@$(HUGO) --gc --minify

check: build
	@test -f public/index.html && test -f public/index.json && echo ok

clean:
	@rm -rf public resources .hugo_build.lock
