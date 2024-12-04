"""This module generates reference docs for pebble CLI commands."""

import logging
import subprocess
import typing


logger = logging.getLogger(__name__)
logging.basicConfig(level=logging.INFO)


def get_all_commands() -> typing.List[str]:
    process = subprocess.run(
        ["go", "run", "../cmd/pebble", "help", "--all"],
        text=True,
        capture_output=True,
        check=True,
    )
    return sorted(
        line.split(maxsplit=1)[0]
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


class Markers:
    def __init__(self, cmd: str):
        self.start = f"<!-- START AUTOMATED OUTPUT FOR {cmd} -->"
        self.end = f"<!-- END AUTOMATED OUTPUT FOR {cmd} -->"


def generate_example(cmd: str, markers: Markers) -> str:
    args = ["help"] if cmd == "help" else [cmd, "--help"]
    help_cmd_str = " ".join(["pebble"] + args)
    go_run_cmd = " ".join(["go", "run", "../cmd/pebble"] + args)
    help_output = get_command_help_output(go_run_cmd).strip()

    return f"""\
{markers.start}
```{{terminal}}
:input: {help_cmd_str}
{help_output}
```
{markers.end}"""


def insert_example(text: str, markers: Markers, example: str) -> str:
    start_pos = text.find(markers.start)
    end_pos = text.find(markers.end) + len(markers.end)
    return text[:start_pos] + example + text[end_pos:]


def process_commands(cmds: typing.List[str]):
    file_path = "reference/cli-commands.md"
    with open(file_path, "r") as file:
        text = file.read()

    for cmd in cmds:
        logger.info(f"Generating help output for '{cmd}'")
        markers = Markers(cmd)
        if markers.start not in text or markers.end not in text:
            logging.error(f"Missing marker for '{cmd}' in {file_path}. Aborting doc generation")
            raise RuntimeError(f"Missing marker for '{cmd}' in {file_path}")

        example = generate_example(cmd, markers)
        text = insert_example(text, markers, example)

    with open(file_path, "w") as file:
        file.write(text)


def main():
    cmds = get_all_commands()
    process_commands(cmds)

    logger.info("Done!")


if __name__ == "__main__":
    main()
