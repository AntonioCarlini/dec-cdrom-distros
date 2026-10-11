#!/usr/bin/env python3
"""
@file
@brief Convert a raw Mode 1 CD-sector image to a 2048-byte-sector image.

This script converts a raw CD image containing 2352-byte Mode 1 sectors
into an image containing only the 2048-byte data payload from each sector.

Before conversion, the script verifies that:
- The input file exists
- The input size is a multiple of 2352 bytes
- Sector 16 contains the Mode 1 header and ISO9660 primary volume
  descriptor signature.

After conversion, the script verifies the ISO9660 primary volume
descriptor in the output image.

The original input file is not modified. The script refuses to overwrite
an existing output file.

Usage:
    python3 raw_cd_to_iso.py input_file
    python3 raw_cd_to_iso.py input_file -o output_file

@param input_file Path to the input raw CD-sector image.
@param output_file Optional path for the converted image. If omitted,
                   "-2048" is appended to the input filename stem.

@note This script assumes 2352-byte Mode 1 sectors with a 16-byte
      header followed by a 2048-byte data payload.
"""

import argparse
from pathlib import Path


def main():
    """Convert a raw CD-sector image to 2048-byte sectors."""
    parser = argparse.ArgumentParser(
        description=(
            "Convert a raw Mode 1 CD image with 2352-byte sectors "
            "to an image with 2048-byte data sectors."
        )
    )
    parser.add_argument(
        "input_file",
        type=Path,
        help="input raw CD-sector image",
    )
    parser.add_argument(
        "-o",
        "--output",
        type=Path,
        help="output image filename (default: append '-2048' to input stem)",
    )
    args = parser.parse_args()

    src = args.input_file
    dst = args.output or src.with_name(f"{src.stem}-2048{src.suffix}")

    sector_size = 2352
    payload_offset = 16
    payload_size = 2048
    pvd_offset = 16 * sector_size

    if not src.is_file():
        raise SystemExit(f"Input file not found: {src}")

    size = src.stat().st_size
    if size % sector_size:
        raise SystemExit("Input size is not a multiple of 2352 bytes")

    with src.open("rb") as f:
        f.seek(pvd_offset)
        sector = f.read(sector_size)

    if len(sector) != sector_size:
        raise SystemExit("Cannot read complete sector 16")

    if sector[15] != 1 or sector[16:22] != b"\x01CD001":
        raise SystemExit(
            "Expected Mode 1 raw sector / ISO9660 descriptor not found. "
            "No output created."
        )

    if dst.exists():
        raise SystemExit(f"Output already exists: {dst}")

    with src.open("rb") as inp, dst.open("xb") as out:
        while True:
            sector = inp.read(sector_size)
            if not sector:
                break
            if len(sector) != sector_size:
                raise SystemExit("Incomplete final sector")
            out.write(sector[payload_offset:payload_offset + payload_size])

    with dst.open("rb") as f:
        f.seek(16 * 2048)
        pvd = f.read(7)

    if pvd != b"\x01CD001\x01":
        dst.unlink()
        raise SystemExit("Converted image failed ISO9660 verification")

    print(f"Created {dst}: {dst.stat().st_size} bytes")


if __name__ == "__main__":
    main()
