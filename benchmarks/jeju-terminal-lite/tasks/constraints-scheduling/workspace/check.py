from pathlib import Path
import hashlib

expected_hashes = {
    "alice_calendar.ics": "5d7ff858d2d742d26b200fc2443d3705f59a049c3c5bd132fcd215329b6a515a",
    "bob_calendar.ics": "1a040cb6b580046466c1b61aed14cabd695e67310d64a6eaf4f0962845ffa54a",
    "carol_calendar.ics": "282030b593b4f12b2bfb67f8872d4f88cf45e06ce654bf89de47d3f985ec1802",
}

for name, expected in expected_hashes.items():
    actual = hashlib.sha256(Path(name).read_bytes()).hexdigest()
    if actual != expected:
        raise SystemExit(f"{name} was modified")

text = Path("meeting_scheduled.ics").read_text()
required = [
    "BEGIN:VCALENDAR",
    "VERSION:2.0",
    "BEGIN:VEVENT",
    "SUMMARY:Team Planning Meeting",
    "DTSTART:20240116T110000Z",
    "DTEND:20240116T120000Z",
    "ATTENDEE:mailto:alice@example.com",
    "ATTENDEE:mailto:bob@example.com",
    "ATTENDEE:mailto:carol@example.com",
    "END:VEVENT",
    "END:VCALENDAR",
]
missing = [item for item in required if item not in text]
if missing:
    raise SystemExit(f"missing ICS fields: {missing}")

print("constraints-scheduling: PASS")
