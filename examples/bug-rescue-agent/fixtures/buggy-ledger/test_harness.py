import io
import json
import os
from pathlib import Path
import unittest


def main():
    os.chdir(Path(__file__).resolve().parent)
    suite = unittest.defaultTestLoader.discover(".", pattern="test_*.py")
    stream = io.StringIO()
    result = unittest.TextTestRunner(stream=stream, verbosity=2).run(suite)
    print(
        json.dumps(
            {
                "status": "passed" if result.wasSuccessful() else "failed",
                "testsRun": result.testsRun,
                "failures": len(result.failures),
                "errors": len(result.errors),
                "output": stream.getvalue(),
            },
            indent=2,
        )
    )


if __name__ == "__main__":
    main()
