"""Loss watcher for the carrier voice run. Two jobs, no side effects on training:

  1. Continuously parse train.log -> loss.csv  (step,loss) for the curve.
  2. Detect a plateau and RAISE A FLAG (PLATEAU_DETECTED) — it does NOT kill
     training. The human/operator makes the actual cut, so a noisy false
     positive can never silently throw away a run.

Runs detached on the pod. Reads only; writes only loss.csv + the flag file.
"""
import os, re, time

LOG = "/workspace/train.log"
CSV = "/workspace/loss.csv"
FLAG = "/workspace/PLATEAU_DETECTED"
LOGGING_STEPS = 10        # matches SFTConfig(logging_steps=10)
MIN_STEP = 800            # don't even consider a plateau before here
WINDOW_STEPS = 200        # compare this-wide loss windows
IMPROVE_THRESH = 0.005    # <0.5% improvement between windows => flat


def parse_pairs():
    try:
        data = open(LOG, errors="ignore").read().replace("\r", "\n")
    except FileNotFoundError:
        return []
    out = []
    for i, m in enumerate(re.finditer(r"'loss': '?([0-9.]+)'?", data)):
        out.append(((i + 1) * LOGGING_STEPS, float(m.group(1))))
    return out


def training_alive():
    for pid in os.listdir("/proc"):
        if not pid.isdigit():
            continue
        try:
            cl = open(f"/proc/{pid}/cmdline", "rb").read().decode(errors="ignore")
        except Exception:
            continue
        if "pod_train_8b.py" in cl:
            return True
    return False


saw_any = False
while True:
    pairs = parse_pairs()
    if pairs:
        saw_any = True
        with open(CSV, "w") as f:
            f.write("step,loss\n")
            for s, l in pairs:
                f.write(f"{s},{l}\n")
        step = pairs[-1][0]
        k = WINDOW_STEPS // LOGGING_STEPS
        if step >= MIN_STEP and len(pairs) >= 2 * k and not os.path.exists(FLAG):
            recent = sum(l for _, l in pairs[-k:]) / k
            prev = sum(l for _, l in pairs[-2 * k:-k]) / k
            improve = (prev - recent) / prev if prev > 0 else 1.0
            if improve < IMPROVE_THRESH:
                with open(FLAG, "w") as f:
                    f.write(f"plateau @ step {step}: last-{WINDOW_STEPS} avg {recent:.4f} "
                            f"vs prior {prev:.4f} = {improve*100:.2f}% improvement\n")
    if saw_any and not training_alive():
        break
    time.sleep(20)
