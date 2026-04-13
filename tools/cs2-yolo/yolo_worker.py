#!/usr/bin/env python3
"""
Minimal TCP inference worker for sfarm CS2 bots.
Protocol (little-endian): u32 display_id, u32 w, u32 h, then w*h*3 RGB bytes.
Response: u32 json_len, UTF-8 JSON {"ok":true,"det":[{"cls":"t","conf":0.9,"xyxy":[x1,y1,x2,y2]}]}
"""
from __future__ import annotations

import argparse
import json
import os
import struct
import sys
import threading
import time

import numpy as np

_last_infer_lock = threading.Lock()
_last_infer_mono = 0.0


def _touch_infer() -> None:
    global _last_infer_mono
    with _last_infer_lock:
        _last_infer_mono = time.monotonic()


def _idle_ping_loop(interval: float, stop: threading.Event) -> None:
    if interval <= 0:
        return
    while not stop.wait(timeout=interval):
        with _last_infer_lock:
            idle = time.monotonic() - _last_infer_mono
        if idle >= interval * 0.82:
            print(
                f"[yolo_worker] idle ping (no inference for {idle:.1f}s)",
                flush=True,
            )


def recv_exact(sock, n: int) -> bytes:
    buf = b""
    while len(buf) < n:
        chunk = sock.recv(n - len(buf))
        if not chunk:
            raise ConnectionError("closed")
        buf += chunk
    return buf


def send_all(sock, data: bytes) -> None:
    off = 0
    while off < len(data):
        n = sock.send(data[off:])
        if n == 0:
            raise ConnectionError("send failed")
        off += n


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--host", default="127.0.0.1")
    ap.add_argument("--port", type=int, default=37771)
    ap.add_argument("--weights", required=True)
    ap.add_argument("--conf", type=float, default=0.28)
    ap.add_argument("--iou", type=float, default=0.45)
    ap.add_argument(
        "--idle-ping",
        type=float,
        default=15.0,
        help="Seconds: print heartbeat when no inference for this long (0=disable)",
    )
    args = ap.parse_args()

    try:
        from ultralytics import YOLO
        import torch
    except ImportError:
        print("pip install ultralytics torch", file=sys.stderr)
        return 1

    wpath = os.path.abspath(args.weights)
    if not os.path.isfile(wpath):
        print(f"Weights not found: {wpath}", file=sys.stderr)
        return 1

    model = YOLO(wpath)
    device = "cuda" if torch.cuda.is_available() else "cpu"
    model.to(device)
    half = device == "cuda"
    # warmup
    model.predict(
        np.zeros((640, 640, 3), dtype=np.uint8),
        verbose=False,
        imgsz=640,
        device=device,
        half=half,
        conf=0.25,
        iou=args.iou,
    )

    _touch_infer()
    CS2_CLASSES = {"c", "ch", "t", "th"}

    import socket

    stop_ping = threading.Event()
    if args.idle_ping > 0:
        threading.Thread(
            target=_idle_ping_loop,
            args=(args.idle_ping, stop_ping),
            daemon=True,
        ).start()

    srv = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    srv.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    srv.bind((args.host, args.port))
    srv.listen(8)
    print(f"[yolo_worker] {wpath} device={device} half={half} listen {args.host}:{args.port}", flush=True)

    try:
        while True:
            conn, _ = srv.accept()
            try:
                while True:
                    hdr = recv_exact(conn, 12)
                    disp, w, h = struct.unpack("<III", hdr)
                    nbytes = w * h * 3
                    raw = recv_exact(conn, nbytes)
                    img = np.frombuffer(raw, dtype=np.uint8).reshape((h, w, 3))
                    results = model.predict(
                        source=img,
                        verbose=False,
                        imgsz=640,
                        device=device,
                        half=half,
                        conf=args.conf,
                        iou=args.iou,
                    )
                    det = []
                    viz = []
                    for r in results:
                        if r.boxes is None:
                            continue
                        for i in range(len(r.boxes.cls)):
                            cls_id = int(r.boxes.cls[i])
                            conf = float(r.boxes.conf[i])
                            name = model.names.get(cls_id, str(cls_id))
                            xy = r.boxes.xyxy[i].tolist()
                            row = {"cls": name, "conf": conf, "xyxy": xy}
                            viz.append(row)
                            if name in CS2_CLASSES:
                                det.append(row)
                    payload = json.dumps(
                        {"ok": True, "display": disp, "det": det, "viz": viz}
                    ).encode("utf-8")
                    send_all(conn, struct.pack("<I", len(payload)) + payload)
                    _touch_infer()
            except (ConnectionError, struct.error, json.JSONDecodeError) as e:
                print(f"[yolo_worker] client session end: {e}", flush=True)
            finally:
                conn.close()
    finally:
        stop_ping.set()


if __name__ == "__main__":
    raise SystemExit(main())
