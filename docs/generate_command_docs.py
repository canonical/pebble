"""This module generate reference docs for pebble CLI commands."""

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
(reference_pebble_{command}_command)=
# {command} command

{description}

## Usage

<!-- START AUTOMATED OUTPUT -->
```{{terminal}}
   :input: command
```
<!-- END AUTOMATED OUTPUT -->
"""


def get_all_commands() -> typing.List[str]:
    process = subprocess.run(
        ["go", "run", "../cmd/pebble", "help", "--all"],
        text=True,
        capture_output=True,
        check=True,
    )
    return sorted(re.findall(r"\n\s{4}([\w-]+)", process.stdout))


def get_command_help_output(cmd: typing.List[str]) -> str:
    return subprocess.run(cmd, text=True, capture_output=True, check=True).stdout


def get_description_from_output(text: str) -> str:
    pattern = r"Usage:\n.*?\n\n(.*?\.)\n.*"
    match = re.search(pattern, text, re.DOTALL)
    if match:
        desired_block = match.group(1).strip()
        return desired_block
    return ""


def render_code_block_cmd(text: str, cmd: str) -> str:
    return re.sub(r"(:input: ).*$", rf"\1{cmd}", text, count=1, flags=re.MULTILINE)


def render_code_block_output(text: str, output: str) -> str:
    start_pos = text.find(AUTOMATED_START_MARKER)
    end_pos = text.find(AUTOMATED_STOP_MARKER) + len(AUTOMATED_STOP_MARKER)
    return text[:start_pos] + output + text[end_pos:]


def update_toc(all_cmds: typing.List[str]):
    index_page = "reference/cli-commands/cli-commands.md"
    with open(index_page, "r") as file:
        text = file.read()

    start_index = text.find("```{toctree}")
    end_index = text.find("```", start_index + 1) + 3
    cmd_list = "\n".join(f"{cmd} <{cmd}>" for cmd in all_cmds)

    toc_tree = f"""\
```{{toctree}}
:titlesonly:
:maxdepth: 1

{cmd_list}
```"""

    text = text[:start_index] + toc_tree + text[end_index:]
    with open(index_page, "w") as file:
        file.write(text)


def create_file_if_not_exist(filepath: str, cmd: str) -> bool:
    file_existed = os.path.exists(filepath)
    if not file_existed:
        logger.info(
            "The doc for command %s doesn't exist, creating from the template.", cmd
        )
        with open(filepath, "w") as file:
            file.write(TEMPLATE)
    return file_existed


def generate_help_command_and_output(cmd: str) -> typing.Tuple[str, str]:
    help_cmd = ["pebble", cmd, "--help"]
    help_cmd_str = " ".join(help_cmd)
    help_cmd_output = get_command_help_output(help_cmd).strip()

    output = f"""\
<!-- START AUTOMATED OUTPUT -->
```{{terminal}}
:input: {help_cmd_str}
{help_cmd_output}
```
<!-- END AUTOMATED OUTPUT -->"""

    return help_cmd, output


def process_command(cmd: str):
    logger.info("Processing doc for command %s.", cmd)

    file_path = f"reference/cli-commands/{cmd}.md"
    file_existed = create_file_if_not_exist(file_path, cmd)

    with open(file_path, "r") as file:
        text = file.read()

    if AUTOMATED_START_MARKER not in text:
        logger.info(
            'The marker for automated doc generation is not found in the "%s" doc, ignore.',
            cmd,
        )
        return

    help_cmd, help_cmd_output = generate_help_command_and_output(cmd)

    if not file_existed:
        text = text.format(
            command=cmd, description=get_description_from_output(help_cmd_output)
        )

    text = render_code_block_cmd(text, help_cmd)
    text = render_code_block_output(text, help_cmd_output)

    with open(file_path, "w") as file:
        file.write(text)


def main():
    all_cmds = get_all_commands()
    for cmd in all_cmds:
        process_command(cmd)

    logger.info("Update toc tree.")
    update_toc(all_cmds)
    logger.info("Done!")


if __name__ == "__main__":
    main()
