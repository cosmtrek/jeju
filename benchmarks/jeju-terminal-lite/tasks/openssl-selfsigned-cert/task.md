Create a self-signed TLS certificate for an internal development server.

Requirements:

1. Create `/app/ssl/`.
2. Generate a 2048-bit RSA private key at `/app/ssl/server.key`.
3. Set private key permissions to `600`.
4. Create a self-signed certificate valid for 365 days:
   - Organization Name: `DevOps Team`
   - Common Name: `dev-internal.company.local`
   - Save it as `/app/ssl/server.crt`.
5. Create `/app/ssl/server.pem` containing the private key and certificate.
6. Create `/app/ssl/verification.txt` containing the certificate subject,
   validity dates, and SHA-256 fingerprint.
7. Create `/app/check_cert.py` that loads the certificate and prints
   `Certificate verification successful` when checks pass.

Run `python3 check.py` to verify.
