#!/usr/bin/env python3
"""
Окно превью для VNC: то же живое ROI, что бот бьёт по X11, с боксами детекций.

Слушает TCP (по умолчанию 127.0.0.1 и порт из --port). Запускать с DISPLAY того же Xvfb, что CS2:

  DISPLAY=:100 python yolo_preview.py --port 37800

Переменная окружения DISPLAY задаётся sfarm при старте бота.
"""
from __future__ import annotations

import argparse
import os
import socket
import struct
import sys

import cv2
import numpy as np

MAGIC = b"YLOP"
# xyxy (5×f32) + u8 cls + 11 bytes UTF-8 name (COCO и др. при cls=255)
DET_SIZE = 32


def recv_exact(sock: socket.socket, n: int) -> bytes:
    buf = b""
    while len(buf) < n:
        c = sock.recv(n - len(buf))
        if not c:
            raise ConnectionError("closed")
        buf += c
    return buf


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--port", type=int, required=True)
    ap.add_argument("--title", default="sfarm YOLO (live X11 ROI)")
    args = ap.parse_args()

    disp = os.environ.get("DISPLAY", "?")
    srv = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    srv.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    srv.bind(("127.0.0.1", args.port))
    srv.listen(2)
    print(f"[yolo_preview] listen 127.0.0.1:{args.port} DISPLAY={disp} (откройте VNC на этот дисплей)", flush=True)

    title = args.title
    cv2.namedWindow(title, cv2.WINDOW_NORMAL)
    cv2.resizeWindow(title, 640, 640)
    cv2.moveWindow(title, 48, 48)

    while True:
        conn, _ = srv.accept()
        try:
            while True:
                mg = recv_exact(conn, 4)
                if mg != MAGIC:
                    raise ValueError(f"bad magic {mg!r}")
                whn = recv_exact(conn, 12)
                w, h, n = struct.unpack("<III", whn)
                det_bytes = n * DET_SIZE
                head = recv_exact(conn, det_bytes)
                rgb = recv_exact(conn, w * h * 3)
                img = np.frombuffer(rgb, dtype=np.uint8).reshape((h, w, 3))
                bgr = cv2.cvtColor(img, cv2.COLOR_RGB2BGR)

                off = 0
                cls_names = ("CT", "CT-h", "T", "T-h", "?")

                def _bgr_for_label(lbl: str) -> tuple[int, int, int]:
                    h = abs(hash(lbl))
                    return (50 + h % 180, 80 + (h // 180) % 160, 100 + (h // 28800) % 150)

                for _ in range(n):
                    chunk = head[off : off + DET_SIZE]
                    off += DET_SIZE
                    x1, y1, x2, y2, conf = struct.unpack("<fffff", chunk[:20])
                    cid = chunk[20]
                    name_raw = chunk[21:32].split(b"\x00", 1)[0].decode("utf-8", "replace")
                    if cid < 4:
                        label = cls_names[cid]
                        color = (
                            (0, 200, 255)
                            if cid == 3
                            else (0, 255, 200)
                            if cid == 1
                            else (80, 180, 255)
                            if cid == 0
                            else (60, 60, 255)
                        )
                    else:
                        label = name_raw or "?"
                        color = _bgr_for_label(label)
                    p1 = (int(x1), int(y1))
                    p2 = (int(x2), int(y2))
                    cv2.rectangle(bgr, p1, p2, color, 2)
                    cv2.putText(
                        bgr,
                        f"{label} {conf:.2f}",
                        (p1[0], max(18, p1[1] - 6)),
                        cv2.FONT_HERSHEY_SIMPLEX,
                        0.45,
                        color,
                        1,
                        cv2.LINE_AA,
                    )

                cv2.putText(
                    bgr,
                    f"{w}x{h} | boxes={n} | X11 live ROI (same as NN)",
                    (6, h - 10),
                    cv2.FONT_HERSHEY_SIMPLEX,
                    0.5,
                    (200, 200, 200),
                    1,
                    cv2.LINE_AA,
                )
                cv2.imshow(title, bgr)
                if cv2.waitKey(1) & 0xFF == ord("q"):
                    conn.close()
                    srv.close()
                    cv2.destroyAllWindows()
                    return 0
        except (ConnectionError, struct.error, ValueError) as e:
            print(f"[yolo_preview] session: {e}", flush=True)
        finally:
            conn.close()


if __name__ == "__main__":
    raise SystemExit(main())
