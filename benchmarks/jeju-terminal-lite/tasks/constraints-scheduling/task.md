Find a 1-hour meeting slot for Alice, Bob, and Carol during January 15-19, 2024.
Business hours are 09:00-18:00 UTC.

Hard constraints:

- Alice is available 09:00-14:00.
- Bob is available 10:00-16:30.
- Carol is available 09:00-17:00, but do not schedule on Monday.
- Avoid all existing VEVENT intervals in `/app/alice_calendar.ics`,
  `/app/bob_calendar.ics`, and `/app/carol_calendar.ics`.
- Do not modify the three input calendar files.

Write `/app/meeting_scheduled.ics` with a 1-hour `Team Planning Meeting`.
It must include attendees `alice@example.com`, `bob@example.com`, and
`carol@example.com`. Use UTC timestamps.

Run `python3 check.py` to verify.
