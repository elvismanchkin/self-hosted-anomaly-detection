"""Score the Go test fixtures with the Python reference scorer (poc/ml/prom_anomaly_job.py).

    ANOMALYD_FIXTURE_DIR=/tmp/fx go test ./internal/detect -run Parity
    python internal/detect/testdata/parity.py /tmp/fx    # needs poc/ml/requirements.txt installed
"""
import json
import pathlib
import sys

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[4] / "poc" / "ml"))
from prom_anomaly_job import score_series  # noqa: E402

END = 1_789_000_000 // 300 * 300
for name in ("normal", "spike", "cold"):
    pts = {int(k): v for k, v in json.loads((pathlib.Path(sys.argv[1]) / f"{name}.json").read_text()).items()}
    r = score_series(pts, END, 300, 86400, 3, 7200, 4, 1e-9, 0.05)
    print(f'"{name}": {{{r["expected"]!r}, {r["score"]!r}}},  // seasonal={r["seasonal"]}')
