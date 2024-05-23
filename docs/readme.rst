Documentation starter pack
==========================

*A pre-configured repository to build and publish documentation with Sphinx.*

The Documentation starter pack includes:

* a bundled Sphinx_ theme, configuration, and extensions
* support for both reStructuredText (reST) and Markdown
* build checks for links, spelling, and inclusive language
* customisation support layered above a core configuration

Quickstart guide
----------------

An initial set of documentation can be built directly from a clone of this
repository.

First, clone this repository to a local directory, and change to this
directory:

.. code-block:: sh

   git clone git@github.com:canonical/sphinx-docs-starter-pack <new-repository-name>
   cd <new-repository-name>

Now build the documentation with the following command. This will create a virtual
environment, install the software dependencies, and build the documentation:

.. code-block:: sh

   make run

Keep this session running to rebuild the documentation automatically whenever a
file is saved, and open |http://127.0.0.1:8000|_ in a web browser to see the
locally built and hosted HTML.

To add a new page to the documentation, create a new document called
``reference.rst`` in a text editor and insert the following reST-formatted
``Reference``  heading:

.. code-block:: rest

    Reference
    =========

Now save ``reference.rst`` and open ``index.rst``.

At the bottom of this file, add an indented line containing ``Reference
<reference>`` to the end of the ``toctree`` section. This is the navigation
title for the new document and its filename without the ``.rst`` extension.

The ``toctree`` section will now look like this:

.. code-block:: rest

    .. toctree::
       :hidden:
       :maxdepth: 2

       ReadMe <readme>
       Reference <reference>

Save ``index.rst`` and reload |http://127.0.0.1:8000|_.

The documentation will now show **Reference** added to the navigation and
selecting this will open the new ``reference.rst`` document.

Contents
--------

The remainder of this README is divided into two main parts: *Enable the starter
pack* and *Work with your documentation* post-enablement.

- `Enable the starter pack`_

  * `Initialise your repository`_

    + `Standalone documentation repository`_
    + `Documentation in a code repository`_
    + `Automation`_

  * `Build the documentation`_
  * `Configure the documentation`_

    + `Configure the header`_
    + `Activate/deactivate feedback button`_
    + `Configure included extensions`_
    + `Add custom configuration`_

  * `Change log`_

- `Work with your documentation`_

  * `Install prerequisite software`_
  * `View the documentation`_

  * `Local checks`_

    + `Local build`_
    + `Spelling check`_
    + `Inclusive language check`_
    + `Accessibility check`_
    + `Link check`_

  * `Configure the spelling check`_
  * `Configure the inclusive language check`_
  * `Configure the accessibility check`_
  * `Configure the link check`_
  * `Add redirects`_
  * `Other resources`_

Enable the starter pack
-----------------------

This section is for repository administrators. It shows how to initialise a
repository with the starter pack. Once this is done, documentation contributors
should follow section `Work with your documentation`_.

**Note:** After setting up your repository with the starter pack, you need to track the changes made to it and manually update your repository with the required files.
The `change log <https://github.com/canonical/sphinx-docs-starter-pack/wiki/Change-log>`_ lists the most relevant (and of course all breaking) changes.
We're planning to provide the contents of this repository as an installable package in the future to make updates easier.

See the `Read the Docs at Canonical <https://library.canonical.com/documentation/read-the-docs>`_ and
`How to publish documentation on Read the Docs <https://library.canonical.com/documentation/publish-on-read-the-docs>`_ guides for
instructions on how to get started with Sphinx documentation.

Initialise your repository
~~~~~~~~~~~~~~~~~~~~~~~~~~

You can either create a standalone documentation project based on this repository or include the files from this repository in a dedicated documentation folder in an existing code repository. The next two sections show the steps needed for each scenario.

See the `Automation`_ section if you would like to have this done via a shell script.

Standalone documentation repository
^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^

To create a standalone documentation repository, clone this starter pack
repository, `update the configuration <#configure-the-documentation>`_, and
then commit all files to the documentation repository.

You don't need to move any files, and you don't need to do any special
configuration on Read the Docs.

Here is one way to do this for newly-created fictional docs repository
``canonical/alpha-docs``:

.. code-block:: none

   git clone git@github.com:canonical/sphinx-docs-starter-pack alpha-docs
   cd alpha-docs
   rm -rf .git
   git init
   git branch -m main
   UPDATE THE CONFIGURATION AND BUILD THE DOCS
   git add -A
   git commit -m "Import sphinx-docs-starter-pack"
   git remote add upstream git@github.com:canonical/alpha-docs
   git push -f upstream main

Documentation in a code repository
^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^

To add documentation to an existing code repository:

#. create a directory called ``docs`` at the root of the code repository
#. populate the above directory with the contents of the starter pack
   repository (with the exception of the ``.git`` directory)
#. copy the file(s) located in the ``docs/.github/workflows`` directory into
   the code repository's ``.github/workflows`` directory
#. in the above workflow file(s), change the value of the ``working-directory`` field from ``.`` to ``docs``
#. create a symbolic link to the ``docs/.wokeignore`` file from the root directory of the code repository
#. in file ``docs/.readthedocs.yaml`` set the following:

   * ``post_checkout: cd docs && python3 .sphinx/build_requirements.py``
   * ``configuration: docs/conf.py``
   * ``requirements: docs/.sphinx/requirements.txt``

**Note:** When configuring RTD itself for your project, the setting "Path for
``.readthedocs.yaml``" (under **Advanced Settings**) will need to be given the
value of ``docs/.readthedocs.yaml``.

Automation
^^^^^^^^^^

To automate the initialisation for either scenario ensure you have the following:

- A GitHub repository where you want to host your documentation, cloned to your local machine. The recommended approach is to host the documentation alongside your code in a ``docs`` folder. But a standalone documentation repository is also an option; in this case, start with an empty repository.
- Git and Bash installed on your system.

There is a provided ``init.sh`` Bash script that does the following:

- clones the starter pack GitHub repository
- creates the specified installation directory if necessary
- updates working directory paths in workflow files, and updates configuration paths in the ``.readthedocs.yaml`` file
- copies and moves contents and ``.github`` files from the starter pack to the installation directory
- deletes the cloned repository when it's done

To use the script:

#. copy ``init.sh`` to your repository's root directory
#. run the script: ``./init.sh``
#. enter the installation directory when prompted. For standalone repositories, enter ".". For documentation alongside code, enter the folder where your documentation is (e.g. ``docs``)

When the script completes, review all changes before committing them.

Build the documentation
~~~~~~~~~~~~~~~~~~~~~~~

The documentation needs to be built in order to be published. This is explained
in more detail in section `Local checks`_ (for contributors), but at this time
you should verify a successful build. Run the following commands from where
your doc files were placed (repository root or the ``docs`` directory):

.. code-block:: none

   make install
   make html

Configure the documentation
~~~~~~~~~~~~~~~~~~~~~~~~~~~

You must modify some of the default configuration to suit your project.
To simplify keeping your documentation in sync with the starter pack, all custom configuration is located in the ``custom_conf.py`` file.
You should never modify the common ``conf.py`` file.

Go through all settings in the ``Project information`` section of the ``custom_conf.py`` file and update them for your project.

See the following sections for further customisation.

Configure the header
^^^^^^^^^^^^^^^^^^^^

By default, the header contains your product tag, product name (taken from the ``project`` setting in the ``custom_conf.py`` file), a link to your product page, and a drop-down menu for "More resources" that contains links to Discourse and GitHub.

You can change any of those links or add further links to the "More resources" drop-down by editing the ``.sphinx/_templates/header.html`` file.
For example, you might want to add links to announcements, tutorials, getting started guides, or videos that are not part of the documentation.

Activate/deactivate feedback button
^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^

A feedback button is included by default, which appears at the top of each page
in the documentation. It redirects users to your GitHub issues page, and
populates an issue for them with details of the page they were on when they
clicked the button.

If your project does not use GitHub issues, set the ``github_issues`` variable
in the ``custom_conf.py`` file to an empty value to disable both the feedback button
and the issue link in the footer.
If you want to deactivate only the feedback button, but keep the link in the
footer, set ``disable_feedback_button`` in the ``custom_conf.py`` file to ``True``.

Configure included extensions
^^^^^^^^^^^^^^^^^^^^^^^^^^^^^

The starter pack includes a set of extensions that are useful for all documentation sets.
They are pre-configured as needed, but you can customise their configuration in the  ``custom_conf.py`` file.

The following extensions are always included:

- |sphinx-design|_
- |sphinx_copybutton|_
- |sphinxcontrib.jquery|_

The following extensions will automatically be included based on the configuration in the ``custom_conf.py`` file:

- |sphinx_tabs.tabs|_
- |sphinx_reredirects|_
- |sphinxext.opengraph|_
- |lxd-sphinx-extensions|_ (``youtube-links``, ``related-links``, ``custom-rst-roles``, and ``terminal-output``)
- |myst_parser|_
- |notfound.extension|_

You can add further extensions in the ``custom_extensions`` variable in ``custom_conf.py``.

Add custom configuration
^^^^^^^^^^^^^^^^^^^^^^^^

To add custom configurations for your project, see the ``Additions to default configuration`` and ``Additional configuration`` sections in the ``custom_conf.py`` file.
These can be used to extend or override the common configuration, or to define additional configuration that is not covered by the common ``conf.py`` file.

The following links can help you with additional configuration:

- `Sphinx configuration`_
- `Sphinx extensions`_
- `Furo documentation`_ (Furo is the Sphinx theme we use as our base.)

Add page-specific configuration
^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^

You can override some global configuration for specific pages.

For example, you can configure whether to display Previous/Next buttons at the bottom of pages in the ``custom_conf.py`` file.
You can then override this default setting for a specific page (a common use case for this is to turn off the Previous/Next buttons by default, but display them in a multi-page tutorial).

To do so, add `file-wide metadata`_ at the top of a page.
See the following examples for how to enable Previous/Next buttons for one page:

reST
  .. code-block::

     :sequential_nav: both

     [Page contents]

MyST
  .. code-block::

     ---
     sequential_nav: both
     ---

     [Page contents]

Possible values for the ``sequential_nav`` field are ``none``, ``prev``, ``next``, and ``both``.
See the ``custom_conf.py`` file for more information.

Another example for page-specific configuration is the ``hide-toc`` field (provided by `Furo <Furo documentation_>`_), which can be used to hide the page-internal table of content.
See `Hiding Contents sidebar`_.

Change log
~~~~~~~~~~

See the `change log <https://github.com/canonical/sphinx-docs-starter-pack/wiki/Change-log>`_ for a list of relevant changes to the starter pack.

Work with your documentation
----------------------------

This section is for documentation contributors. It assumes that the current
repository has been initialised with the starter pack as described in
section `Enable the starter pack`_.

There are make targets defined in the ``Makefile`` that do various things. To
get started, we will:

* install prerequisite software
* view the documentation

Install prerequisite software
~~~~~~~~~~~~~~~~~~~~~~~~~~~~~

Before you start, make sure you have ``make`` and ``python3`` on your system:

.. code-block:: none

   sudo apt update
   sudo apt install make python3

Some `validation tools <#local-checks>`_ won't be available by default;
to install them, you need ``snap`` and ``npm``:

.. code-block:: none

   sudo apt install npm snapd

To install the core prerequisites:

.. code-block:: none

   make install

This will create the required software list (``.sphinx/requirements.txt``),
which is used to create a virtual environment (``.sphinx/venv``) and install
dependency software within it.

To install the validation tools:

.. code-block:: none

   make woke-install
   make pa11y-install

You can add further Python modules to the required software list
(``.sphinx/requirements.txt``) in the ``custom_required_modules`` variable
in the ``custom_conf.py`` file.

**Note**:
By default, the starter pack uses the latest compatible version of all tools and does not pin its requirements.
This might change temporarily if there is an incompatibility with a new tool version.
There is therefore no need in using a tool like Renovate to automatically update the requirements.

View the documentation
~~~~~~~~~~~~~~~~~~~~~~

To view the documentation:

.. code-block:: none

   make run

This will do several things:

* activate the virtual environment
* build the documentation
* serve the documentation on **127.0.0.1:8000**
* rebuild the documentation each time a file is saved
* send a reload page signal to the browser when the documentation is rebuilt

The ``run`` target is therefore very convenient when preparing to submit a
change to the documentation.

.. note::

   If you encounter the error ``locale.Error: unsupported locale setting`` when activating the Python virtual environment, include the environment variable in the command and try again: ``LC_ALL=en_US.UTF-8 make run``

Local checks
~~~~~~~~~~~~

Before committing and pushing changes, it's a good practice to run various checks locally to catch issues early in the development process.

Local build
^^^^^^^^^^^

Run a clean build of the docs to surface any build errors that would occur in RTD:

.. code-block:: none

   make clean-doc
   make html

Spelling check
^^^^^^^^^^^^^^

Ensure there are no spelling errors in the documentation:

.. code-block:: shell

   make spelling

Inclusive language check
^^^^^^^^^^^^^^^^^^^^^^^^

Ensure the documentation uses inclusive language:

.. code-block:: shell

   make woke

Accessibility check
^^^^^^^^^^^^^^^^^^^

Look for accessibility issues in rendered documentation:

.. code-block:: shell

   make pa11y

Link check
^^^^^^^^^^

Validate links within the documentation:

.. code-block:: shell

   make linkcheck

Configure the spelling check
~~~~~~~~~~~~~~~~~~~~~~~~~~~~

The spelling check uses ``aspell``.
Its configuration is located in the ``.sphinx/spellingcheck.yaml`` file.

To add exceptions for words flagged by the spelling check, edit the ``.custom_wordlist.txt`` file.
You shouldn't edit ``.wordlist.txt``, because this file is maintained and updated centrally and contains words that apply across all projects.

Configure the inclusive language check
~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~

By default, the inclusive language check is applied only to reST files located
under the documentation directory (usually ``docs``). To check Markdown files,
for example, or to use a location other than the ``docs`` sub-tree, you must
change how the ``woke`` tool is invoked from within ``docs/Makefile`` (see
the `woke User Guide <https://docs.getwoke.tech/usage/#file-globs>`_ for help).

Inclusive language check exemptions
^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^

Some circumstances may require you to use some non-inclusive words. In such
cases you will need to create check-exemptions for them.

This page provides an overview of two inclusive language check exemption
methods for files written in reST format. See the `woke documentation`_ for
full coverage.

Exempt a word
.............

To exempt an individual word, place a custom ``none`` role (defined in the
``canonical-sphinx-extensions`` Sphinx extension) anywhere on the line
containing the word in question. The role syntax is:

.. code-block:: none

   :none:`wokeignore:rule=<SOME_WORD>,`

For instance:

.. code-block:: none

   This is your text. The word in question is here: whitelist. More text. :none:`wokeignore:rule=whitelist,`

To exempt an element of a URL, it is recommended to use the standard reST
method of placing links at the bottom of the page (or in a separate file). In
this case, a comment line is placed immediately above the URL line. The comment
syntax is:

.. code-block:: none

   .. wokeignore:rule=<SOME_WORD>

Here is an example where a URL element contains the string "master": :none:`wokeignore:rule=master,`

.. code-block:: none

   .. LINKS
   .. wokeignore:rule=master
   .. _link definition: https://some-external-site.io/master/some-page.html

You can now refer to the label ``link definition_`` in the body of the text.

Exempt an entire file
.....................

A more drastic solution is to make an exemption for the contents of an entire
file. For example, to exempt file ``docs/foo/bar.rst`` add the following line
to file ``.wokeignore``:

.. code-block:: none

   foo/bar.rst

Configure the accessibility check
~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~

The ``pa11y.json`` file at the starter pack root provides basic defaults; to
browse the available settings and options, see ``pa11y``'s `README
<https://github.com/pa11y/pa11y#command-line-configuration>`_ on GitHub.


Configure the link check
~~~~~~~~~~~~~~~~~~~~~~~~

If you have links in the documentation that you don't want to be checked (for
example, because they are local links or give random errors even though they
work), you can add them to the ``linkcheck_ignore`` variable in the ``custom_conf.py`` file.

Add redirects
~~~~~~~~~~~~~

You can add redirects to make sure existing links and bookmarks continue working when you move files around.
To do so, specify the old and new paths in the ``redirects`` setting of the ``custom_conf.py`` file.

Other resources
~~~~~~~~~~~~~~~

- `Example product documentation <https://canonical-example-product-documentation.readthedocs-hosted.com/>`_
- `Example product documentation repository <https://github.com/canonical/example-product-documentation>`_

.. LINKS

.. wokeignore:rule=master
.. _`Sphinx configuration`: https://www.sphinx-doc.org/en/master/usage/configuration.html
.. wokeignore:rule=master
.. _`Sphinx extensions`: https://www.sphinx-doc.org/en/master/usage/extensions/index.html
.. wokeignore:rule=master
.. _`file-wide metadata`: https://www.sphinx-doc.org/en/master/usage/restructuredtext/field-lists.html
.. _`Furo documentation`: https://pradyunsg.me/furo/quickstart/
.. _`Hiding Contents sidebar`: https://pradyunsg.me/furo/customisation/toc/
.. _`Sphinx`: https://www.sphinx-doc.org/

.. |http://127.0.0.1:8000| replace:: ``http://127.0.0.1:8000``
.. _`http://127.0.0.1:8000`: http://127.0.0.1:8000
.. |sphinx-design| replace:: ``sphinx-design``
.. _sphinx-design: https://sphinx-design.readthedocs.io/en/latest/
.. |sphinx_tabs.tabs| replace:: ``sphinx_tabs.tabs``
.. _sphinx_tabs.tabs: https://sphinx-tabs.readthedocs.io/en/latest/
.. |sphinx_reredirects| replace:: ``sphinx_reredirects``
.. _sphinx_reredirects: https://documatt.gitlab.io/sphinx-reredirects/
.. |lxd-sphinx-extensions| replace:: ``lxd-sphinx-extensions``
.. _lxd-sphinx-extensions: https://github.com/canonical/lxd-sphinx-extensions#lxd-sphinx-extensions
.. |sphinx_copybutton| replace:: ``sphinx_copybutton``
.. _sphinx_copybutton: https://sphinx-copybutton.readthedocs.io/en/latest/
.. |sphinxext.opengraph| replace:: ``sphinxext.opengraph``
.. _sphinxext.opengraph: https://sphinxext-opengraph.readthedocs.io/en/latest/
.. |myst_parser| replace:: ``myst_parser``
.. _myst_parser: https://myst-parser.readthedocs.io/en/latest/
.. |sphinxcontrib.jquery| replace:: ``sphinxcontrib.jquery``
.. _sphinxcontrib.jquery: https://github.com/sphinx-contrib/jquery/
.. |notfound.extension| replace:: ``notfound.extension``
.. _notfound.extension: https://sphinx-notfound-page.readthedocs.io/en/latest/

.. _woke documentation: https://docs.getwoke.tech/ignore
