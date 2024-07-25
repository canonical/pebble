import logging
import os
import re
import subprocess
import typing


logger = logging.getLogger(__name__)
logging.basicConfig(level=logging.INFO)

AUTOMATED_START_MARKER = "<!-- START AUTOMATED OUTPUT -->"
AUTOMATED_STOP_MARKER = "<!-- END AUTOMATED OUTPUT -->"

TEMPLATE = """\
# CMD command

Description

## Usage

<!-- START AUTOMATED OUTPUT -->
```{terminal}
   :input: command
```
<!-- END AUTOMATED OUTPUT -->
"""


def get_all_pebble_commands() -> typing.List[str]:
    cmd = "pebble help --all"
    p = subprocess.run(cmd.split(), text=True, capture_output=True)
    if p.returncode != 0:
        logging.error("Error running {}.".format(cmd))
        exit(1)
    return re.findall(r"\n\s{4}(\w+)", p.stdout)


def get_command_help_output(cmd: str) -> str:
    p = subprocess.run(cmd.split(), text=True, capture_output=True)
    if p.returncode != 0:
        logging.error("Error running {}.".format(cmd))
        exit(1)
    return p.stdout


def get_description_from_output(text: str) -> str:
    pattern = r"Usage:\n.*?\n\n(.*?)\n.*"
    match = re.search(pattern, text, re.DOTALL)
    if match:
        desired_block = match.group(1).strip()
        return desired_block
    return ""


def render_title(text: str, cmd: str) -> str:
    return re.sub(
        r"^# CMD command$",
        f"# {cmd.capitalize()} command",
        text,
        flags=re.MULTILINE,
    )


def render_description(text: str, description: str) -> str:
    return re.sub(r"^Description$", f"{description}", text, flags=re.MULTILINE)


def render_code_block_cmd(text: str, cmd: str) -> str:
    return re.sub(r"(:input: ).*$", rf"\1{cmd}", text, count=1, flags=re.MULTILINE)


def render_code_block_output(text: str, output: str) -> str:
    start_pos = text.find(AUTOMATED_START_MARKER)
    end_pos = text.find(AUTOMATED_STOP_MARKER) + len(AUTOMATED_STOP_MARKER)
    return text[:start_pos] + output + text[end_pos:]


def update_toc(all_commands):
    index_page = "reference/cli-commands/cli-commands.md"
    with open(index_page, "r") as file:
        text = file.read()

    start_index = text.find("```{toctree}")
    end_index = text.find("```", start_index + 1) + 3
    cmd_list = "\n".join(["{cmd} <{cmd}>".format(cmd=cmd) for cmd in all_commands])
    toc_tree = """\
```{{toctree}}
:titlesonly:
:maxdepth: 1

{cmd_list}
```""".format(cmd_list=cmd_list)

    text = text[:start_index] + toc_tree + text[end_index:]
    with open(index_page, "w") as file:
        file.write(text)


def main():
    all_commands = get_all_pebble_commands()
    for command in all_commands:
        logger.info("Processing doc for command {}".format(command))

        file_path = "reference/cli-commands/{}.md".format(command)
        file_existed = os.path.exists(file_path)
        if not file_existed:
            logger.info(
                "The doc for command {} doesn't exist, "
                "creating from the template.".format(command)
            )
            with open(file_path, "w") as file:
                file.write(TEMPLATE)

        with open(file_path, "r") as file:
            text = file.read()

        if AUTOMATED_START_MARKER not in text:
            logger.info(
                "The marker for automated doc generation is not found in the doc, ignore."
            )
            continue

        help_cmd = "pebble {} --help".format(command)

        help_cmd_output = """\
<!-- START AUTOMATED OUTPUT -->
```{{terminal}}
:input: {help_cmd}
{stdout}
```
<!-- END AUTOMATED OUTPUT -->""".format(
            help_cmd=help_cmd,
            stdout=get_command_help_output(help_cmd).strip(),
        )

        text = render_code_block_cmd(text, help_cmd)
        text = render_code_block_output(text, help_cmd_output)

        if not file_existed:
            text = render_title(text, command)
            text = render_description(
                text, get_description_from_output(help_cmd_output)
            )

        with open(file_path, "w") as file:
            file.write(text)

    logger.info("Update toc tree.")
    update_toc(all_commands)
    logger.info("Done!")


if __name__ == "__main__":
    main()
