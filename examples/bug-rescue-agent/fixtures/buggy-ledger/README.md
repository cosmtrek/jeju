# Orbital Ledger

Orbital Ledger is a tiny accounting module for mission budget events. Amounts
are stored as integer cents so downstream reports never depend on floating-point
state.

Expected behavior:

- `Ledger.post(account, amount, memo)` records one entry.
- `Ledger.transfer(source, destination, amount, memo)` records a balanced debit
  and credit.
- Amounts are rounded to cents with normal half-up financial rounding.
- `Ledger.balance(account)` returns the account balance in cents.

Run the tests with:

```bash
python3 test_harness.py
```
