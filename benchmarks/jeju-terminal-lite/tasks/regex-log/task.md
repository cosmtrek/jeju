Write a regex expression that matches dates in the format YYYY-MM-DD appearing
in lines that contain a valid IPv4 address. If multiple dates are present in one
line, match only the last valid date in that line.

Avoid false matches where the date or IP address is immediately adjacent to
letters or digits. Save only the regex pattern to `/app/regex.txt`.

Use `/app/sample.log` for iteration. You can run `python3 /app/check.py` to
verify the result. In Jeju shell commands, run that as `python3 check.py`.
