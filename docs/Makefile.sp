# Minimal makefile for Sphinx documentation
#

# You can set these variables from the command line, and also
# from the environment for the first two.
SPHINXDIR     = .sphinx
SPHINXOPTS    ?= -c . -d $(SPHINXDIR)/.doctrees
SPHINXBUILD   ?= sphinx-build
SOURCEDIR     = .
BUILDDIR      = _build
VENVDIR       = $(SPHINXDIR)/venv
PA11Y         = $(SPHINXDIR)/node_modules/pa11y/bin/pa11y.js --config $(SPHINXDIR)/pa11y.json
VENV          = $(VENVDIR)/bin/activate

.PHONY: sp-full-help sp-woke-install sp-pa11y-install sp-install sp-run sp-html \
        sp-epub sp-serve sp-clean sp-clean-doc sp-spelling sp-linkcheck sp-woke \
        sp-pa11y Makefile.sp

sp-full-help: $(VENVDIR)
	@. $(VENV); $(SPHINXBUILD) -M help "$(SOURCEDIR)" "$(BUILDDIR)" $(SPHINXOPTS) $(O)
	@echo "\n\033[1;31mNOTE: This help texts shows unsupported targets!\033[0m"
	@echo "Run 'make help' to see supported targets."

# Shouldn't assume that venv is available on Ubuntu by default; discussion here:
# https://bugs.launchpad.net/ubuntu/+source/python3.4/+bug/1290847
$(SPHINXDIR)/requirements.txt:
	python3 $(SPHINXDIR)/build_requirements.py
	python3 -c "import venv" || sudo apt install python3-venv

# If requirements are updated, venv should be rebuilt and timestamped.
$(VENVDIR): $(SPHINXDIR)/requirements.txt
	@echo "... setting up virtualenv"
	python3 -m venv $(VENVDIR)
	. $(VENV); pip install --require-virtualenv \
	    --upgrade -r $(SPHINXDIR)/requirements.txt \
            --log $(VENVDIR)/pip_install.log
	@test ! -f $(VENVDIR)/pip_list.txt || \
            mv $(VENVDIR)/pip_list.txt $(VENVDIR)/pip_list.txt.bak
	@. $(VENV); pip list --local --format=freeze > $(VENVDIR)/pip_list.txt
	@touch $(VENVDIR)

sp-woke-install:
	@type woke >/dev/null 2>&1 || \
            { echo "Installing \"woke\" snap... \n"; sudo snap install woke; }

sp-pa11y-install:
	@type $(PA11Y) >/dev/null 2>&1 || { \
			echo "Installing \"pa11y\" from npm... \n"; \
			mkdir -p $(SPHINXDIR)/node_modules/ ; \
			npm install --prefix $(SPHINXDIR) pa11y; \
		}

sp-install: $(VENVDIR)
	sudo apt-get update
	sudo apt-get install --assume-yes distro-info

sp-run: sp-install
	. $(VENV); sphinx-autobuild -b dirhtml "$(SOURCEDIR)" "$(BUILDDIR)" $(SPHINXOPTS)

# Doesn't depend on $(BUILDDIR) to rebuild properly at every run.
sp-html: sp-install
	. $(VENV); $(SPHINXBUILD) -W --keep-going -b dirhtml "$(SOURCEDIR)" "$(BUILDDIR)" -w $(SPHINXDIR)/warnings.txt $(SPHINXOPTS)

sp-epub: sp-install
	. $(VENV); $(SPHINXBUILD) -b epub "$(SOURCEDIR)" "$(BUILDDIR)" -w $(SPHINXDIR)/warnings.txt $(SPHINXOPTS)

sp-serve: sp-html
	cd "$(BUILDDIR)"; python3 -m http.server 8000

sp-clean: sp-clean-doc
	@test ! -e "$(VENVDIR)" -o -d "$(VENVDIR)" -a "$(abspath $(VENVDIR))" != "$(VENVDIR)"
	rm -rf $(VENVDIR)
	rm -f $(SPHINXDIR)/requirements.txt
	rm -rf $(SPHINXDIR)/node_modules/

sp-clean-doc:
	git clean -fx "$(BUILDDIR)"
	rm -rf $(SPHINXDIR)/.doctrees

sp-spelling: sp-html
	. $(VENV) ; python3 -m pyspelling -c $(SPHINXDIR)/spellingcheck.yaml -j $(shell nproc)

sp-linkcheck: sp-install
	. $(VENV) ; $(SPHINXBUILD) -b linkcheck "$(SOURCEDIR)" "$(BUILDDIR)" $(SPHINXOPTS)

sp-woke: sp-woke-install
	woke *.rst **/*.rst --exit-1-on-failure \
	    -c https://github.com/canonical/Inclusive-naming/raw/main/config.yml

sp-pa11y: sp-pa11y-install sp-html
	find $(BUILDDIR) -name *.html -print0 | xargs -n 1 -0 $(PA11Y)

# Catch-all target: route all unknown targets to Sphinx using the new
# "make mode" option.  $(O) is meant as a shortcut for $(SPHINXOPTS).
%: Makefile.sp
	. $(VENV); $(SPHINXBUILD) -M $@ "$(SOURCEDIR)" "$(BUILDDIR)" $(SPHINXOPTS) $(O)
