#!/usr/bin/env python3
import argparse
import json
from datetime import date, timedelta


def main():
    parser = argparse.ArgumentParser(description="Shift an ISO date by a number of days.")
    parser.add_argument("--date", required=True)
    parser.add_argument("--days", required=True, type=int)
    args = parser.parse_args()

    shifted = date.fromisoformat(args.date) + timedelta(days=args.days)
    print(json.dumps({
        "ok": True,
        "output": {
            "date": shifted.isoformat()
        }
    }))


if __name__ == "__main__":
    main()
