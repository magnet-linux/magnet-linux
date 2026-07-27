SCDOC ?= scdoc

SOURCES := \
	magpkg.1.scd \
	magpkg-build.1.scd \
	magpkg-clean.1.scd \
	magpkg-init.1.scd \
	magpkg-sync.1.scd \
	magpkg-uninstall.1.scd \
	magpkg-package.5.scd \
	magpkg-store.5.scd
PAGES := $(SOURCES:.scd=)

all: $(PAGES)

check:
	@for source in $(SOURCES); do \
		$(SCDOC) < "$$source" > /dev/null || exit; \
	done

%.1: %.1.scd
	$(SCDOC) < $< > $@

%.5: %.5.scd
	$(SCDOC) < $< > $@

clean:
	rm -f $(PAGES)

.PHONY: all check clean
