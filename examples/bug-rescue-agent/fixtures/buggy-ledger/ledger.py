from decimal import Decimal


class Ledger:
    def __init__(self):
        self.entries = []

    def post(self, account, amount, memo):
        cents = int(Decimal(str(amount)) * 100)
        self.entries.append(
            {
                "account": account,
                "cents": cents,
                "memo": memo,
            }
        )
        return cents

    def transfer(self, source, destination, amount, memo):
        debit = self.post(source, -amount, f"{memo} debit")
        credit = self.post(destination, amount, f"{memo} credit")
        return debit + credit

    def balance(self, account):
        return sum(entry["cents"] for entry in self.entries if entry["account"] == account)
