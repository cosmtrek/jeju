from decimal import Decimal
import unittest

from ledger import Ledger


class LedgerTest(unittest.TestCase):
    def test_post_rounds_half_up_to_cents(self):
        ledger = Ledger()

        stored = ledger.post("oxygen", "10.015", "top up tank reserve")

        self.assertEqual(stored, 1002)
        self.assertEqual(ledger.balance("oxygen"), 1002)

    def test_transfer_stays_balanced_after_rounding(self):
        ledger = Ledger()

        movement = ledger.transfer("mission-control", "orbital-cache", Decimal("2.675"), "supply packet")

        self.assertEqual(movement, 0)
        self.assertEqual(ledger.balance("mission-control"), -268)
        self.assertEqual(ledger.balance("orbital-cache"), 268)


if __name__ == "__main__":
    unittest.main()
