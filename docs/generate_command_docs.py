"""This module generates reference docs for pebble CLI commands."""

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


def get_all_commands() -> typing.List[typing.Tuple[str, str]]:
    process = subprocess.run(
        ["go", "run", "../cmd/pebble", "help", "--all"],
        text=True,
        capture_output=True,
        check=True,
    )
    return sorted(
        line.split(maxsplit=1)
        for line in process.stdout.splitlines()
        if line.startswith("    ")
    )


def get_command_help_output(cmd: str) -> str:
    # Set a fixed terminal line columns so that the output won't be
    # affected by the actual terminal width.
    cmd = f"stty cols 80; {cmd}"
    return subprocess.run(
        cmd,
        shell=True,
        text=True,
        capture_output=True,
        check=True,
    ).stdout


def render_code_block_cmd(text: str, cmd: str) -> str:
    return re.sub(r"(:input: ).*$", rf"\1{cmd}", text, count=1, flags=re.MULTILINE)


def render_code_block_output(text: str, output: str) -> str:
    start_pos = text.find(AUTOMATED_START_MARKER)
    end_pos = text.find(AUTOMATED_STOP_MARKER) + len(AUTOMATED_STOP_MARKER)
    return text[:start_pos] + output + text[end_pos:]


def update_toc(cmds: typing.List[typing.Tuple[str, str]]):
    index_page = "reference/cli-commands/cli-commands.md"
    with open(index_page, "r") as file:
        text = file.read()

    start_index = text.find("```{toctree}")
    end_index = text.find("```", start_index + 1) + 3
    cmd_list = "\n".join(f"{cmd[0]} <{cmd[0]}>" for cmd in cmds)

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
    args = ["help"] if cmd == "help" else [cmd, "--help"]
    help_cmd_str = " ".join(["pebble"] + args)
    go_run_cmd = " ".join(["go", "run", "../cmd/pebble"] + args)
    help_cmd_output = get_command_help_output(go_run_cmd).strip()

    output = f"""\
<!-- START AUTOMATED OUTPUT -->
```{{terminal}}
:input: {help_cmd_str}
{help_cmd_output}
```
<!-- END AUTOMATED OUTPUT -->"""

    return help_cmd_str, output


def process_command(cmd: str, description: str):
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
    description = f"The `{cmd}` command is used to {description.lower()}."

    if not file_existed:
        text = text.format(command=cmd, description=description)

    text = render_code_block_cmd(text, help_cmd)
    text = render_code_block_output(text, help_cmd_output)

    with open(file_path, "w") as file:
        file.write(text)


def main():
    cmds = get_all_commands()
    for cmd, description in cmds:
        process_command(cmd, description)

    logger.info("Update toc tree.")
    update_toc(cmds)
    logger.info("Done!")


if __name__ == "__main__":
    main()
