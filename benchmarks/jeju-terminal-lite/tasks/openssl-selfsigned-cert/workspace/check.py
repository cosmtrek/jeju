import os
import stat
import subprocess
from pathlib import Path

key = Path("ssl/server.key")
crt = Path("ssl/server.crt")
pem = Path("ssl/server.pem")
verification = Path("ssl/verification.txt")
checker = Path("check_cert.py")

for path in [key, crt, pem, verification, checker]:
    if not path.exists():
        raise SystemExit(f"missing {path}")

mode = stat.S_IMODE(os.stat(key).st_mode)
if mode != 0o600:
    raise SystemExit(f"server.key permissions must be 600, got {oct(mode)}")

subject = subprocess.check_output(
    ["openssl", "x509", "-in", str(crt), "-noout", "-subject"],
    text=True,
)
if "DevOps Team" not in subject or "dev-internal.company.local" not in subject:
    raise SystemExit(f"bad subject: {subject}")

modulus_key = subprocess.check_output(["openssl", "rsa", "-in", str(key), "-noout", "-modulus"], text=True)
modulus_crt = subprocess.check_output(["openssl", "x509", "-in", str(crt), "-noout", "-modulus"], text=True)
if modulus_key != modulus_crt:
    raise SystemExit("key and certificate do not match")

pem_text = pem.read_text()
if "BEGIN PRIVATE KEY" not in pem_text and "BEGIN RSA PRIVATE KEY" not in pem_text:
    raise SystemExit("PEM missing private key")
if "BEGIN CERTIFICATE" not in pem_text:
    raise SystemExit("PEM missing certificate")

verify_text = verification.read_text()
for token in ["subject", "notBefore", "notAfter", "sha256 Fingerprint"]:
    if token not in verify_text:
        raise SystemExit(f"verification.txt missing {token}")

output = subprocess.check_output(["python3", str(checker)], text=True)
if "Certificate verification successful" not in output:
    raise SystemExit(f"check_cert.py did not report success: {output}")

print("openssl-selfsigned-cert: PASS")
