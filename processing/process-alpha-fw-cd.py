#!/usr/bin/env python3

"""
@file smmtable_to_wiki.py
@brief Convert a DEC Alpha Firmware CD SMMTABLE.TXT file into a MediaWiki table.

@details
Reads firmware mapping entries from a DEC Alpha firmware CD's
SMMTABLE.TXT file, decodes the firmware flags, and generates a
sortable MediaWiki table.

The input is read as Latin-1 and the output is UTF-8.

Each firmware entry must contain a platform comment introduced by
an exclamation mark. Entries with missing or invalid fields, or
unsupported flag bits, cause processing to fail.

The generated table contains the following columns:
- Family
- Processor
- SMM values
- Default firmware
- Flags (hex)
- Decoded flags
- SRM version
- Platform

@note
No output file is produced if parsing fail and any existing output file is left unchanged.
"""

import argparse
import re
import sys
from pathlib import Path


ENTRY_RE = re.compile(
    r"(\[[^\]]+\][^\s]+)\s+"
    r"([0-9]+)\s+"
    r"(\S+)\s*$"
)


def parse_entry(line: str, line_number: int) -> dict[str, str]:
    """Parse one firmware mapping line."""
    data, separator, comment = line.partition("!")

    if not separator:
        raise ValueError("missing platform comment")

    fields = data.split(maxsplit=3)
    if len(fields) != 4:
        raise ValueError("expected family, processor, SMM values and firmware")

    family, processor, smm_values, remainder = fields

    # V3.8 format: firmware followed by a single boot-flags selector.
    old_match = re.fullmatch(
        r"(\[[^\]]+\][^\s]+)\s+([01])\s*",
        remainder,
    )

    if old_match:
        firmware, boot_selector = old_match.groups()
        flags = int(boot_selector)

        boot_flags = "0x80" if flags == 1 else "0xA0"

        return {
            "family": family,
            "processor": processor,
            "smm": smm_values,
            "firmware": firmware,
            "flags": f"0x{flags:X}",
            "decoded": f"Boot flags 0,{boot_flags}",
            "srm_version": "",
            "platform": comment.strip(),
        }

    # Later format: firmware, flags bitmask and SRM version.
    match = ENTRY_RE.search(remainder)
    if not match:
        raise ValueError("could not parse firmware, flags and SRM version")

    firmware, flags_text, srm_version = match.groups()
    flags = int(flags_text, 16)

    if flags & ~0x7:
        raise ValueError(f"unexpected flag bits set: 0x{flags:X}")

    boot_flags = "0x80" if flags & 0x1 else "0xA0"
    software_reset = "Yes" if flags & 0x2 else "No"
    silent_update = "Yes" if flags & 0x4 else "No"

    return {
        "family": family,
        "processor": processor,
        "smm": smm_values,
        "firmware": firmware,
        "flags": f"0x{flags:X}",
        "decoded": (
            f"Boot flags 0,{boot_flags}; "
            f"software reset: {software_reset}; "
            f"silent update: {silent_update}"
        ),
        "srm_version": srm_version,
        "platform": comment.strip(),
    }

def make_wiki_table(entries: list[dict[str, str]]) -> str:
    """Render parsed entries as a MediaWiki table."""
    columns = [
        ("Family", "family"),
        ("Processor", "processor"),
        ("SMM values", "smm"),
        ("Default firmware", "firmware"),
        ("Flags (hex)", "flags"),
        ("Decoded flags", "decoded"),
        ("SRM version", "srm_version"),
        ("Platform", "platform"),
    ]

    output = [
        '{| class="wikitable sortable"',
        "|-",
        "! " + " !! ".join(header for header, _ in columns),
    ]

    for entry in entries:
        output.append("|-")
        cells = []
        for _, key in columns:
            value = entry[key]
            if key in {"firmware", "flags", "srm_version"}:
                value = f"<code>{value}</code>"
            cells.append(value)
        output.append("| " + " || ".join(cells))

    output.append("|}")
    return "\n".join(output) + "\n"


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Convert DEC Alpha SMMTABLE.TXT to MediaWiki."
    )
    parser.add_argument("input", type=Path, help="Source SMMTABLE.TXT")
    parser.add_argument(
        "-o", "--output", type=Path, default=Path("SMMTABLE.wiki"),
        help="Output file (default: SMMTABLE.wiki)",
    )
    args = parser.parse_args()

    entries = []

    try:
        with args.input.open(encoding="latin-1") as source:
            for line_number, line in enumerate(source, start=1):
                if not line.strip() or line.lstrip().startswith("!"):
                    continue

                try:
                    entries.append(parse_entry(line, line_number))
                except ValueError as exc:
                    raise ValueError(
                        f"{args.input}:{line_number}: {exc}"
                    ) from exc

        if not entries:
            raise ValueError("no firmware entries found")

        args.output.write_text(
            make_wiki_table(entries),
            encoding="utf-8",
        )

    except (OSError, ValueError) as exc:
        print(f"Error: {exc}", file=sys.stderr)
        return 1

    print(f"Wrote {len(entries)} entries to {args.output}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
