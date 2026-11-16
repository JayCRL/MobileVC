#!/usr/bin/env python3

from __future__ import annotations

import argparse
import html
from pathlib import Path


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Render MobileVC install page.")
    parser.add_argument("--output", required=True)
    parser.add_argument("--ios-url")
    parser.add_argument("--ios-version")
    parser.add_argument("--ios-bundle-id")
    parser.add_argument("--testflight-url")
    parser.add_argument("--testflight-version")
    parser.add_argument("--testflight-bundle-id")
    parser.add_argument("--android-url")
    parser.add_argument("--android-version")
    parser.add_argument("--android-package-id")
    return parser.parse_args()


def escape(value: str) -> str:
    return html.escape(html.unescape(value), quote=True)


def render_link_line(label: str, url: str) -> str:
    return f"""
          <p class="link-line">
            <span class="link-label">{escape(label)}</span>
            <a class="link-text" href="{escape(url)}">{escape(url)}</a>
          </p>
    """


def render_ios(url: str, version: str, bundle_id: str) -> str:
    return f"""
        <article class="platform-card" data-platform="ios" data-url="{escape(url)}" data-version="{escape(version)}" data-package="{escape(bundle_id)}">
          <div class="platform-head">
            <span class="platform-tag">iPhone</span>
            <span class="platform-version">{escape(version)}</span>
          </div>
          <h2>iPhone 安装</h2>
          <p>请用 Safari 打开并安装，安装后如首次打不开，到“设置 - 通用 - VPN 与设备管理”完成信任。</p>
          <a class="button" href="{escape(url)}">一键安装</a>
          <p class="meta-line">应用标识：<code>{escape(bundle_id)}</code></p>
{render_link_line("安装地址", url)}
        </article>
    """


def render_testflight(url: str, version: str, bundle_id: str) -> str:
    return f"""
        <article class="platform-card" data-platform="testflight" data-url="{escape(url)}" data-version="{escape(version)}" data-package="{escape(bundle_id)}">
          <div class="platform-head">
            <span class="platform-tag testflight">TestFlight</span>
            <span class="platform-version">{escape(version)}</span>
          </div>
          <h2>TestFlight 安装</h2>
          <p>通过 TestFlight 安装 iPhone 测试版本。先安装 Apple TestFlight，再打开邀请链接加入测试版本。</p>
          <a class="button secondary" href="{escape(url)}">加入 TestFlight</a>
          <p class="meta-line">应用标识：<code>{escape(bundle_id)}</code></p>
{render_link_line("邀请链接", url)}
        </article>
    """


def render_android(url: str, version: str, package_id: str) -> str:
    return f"""
        <article class="platform-card" data-platform="android" data-url="{escape(url)}" data-version="{escape(version)}" data-package="{escape(package_id)}">
          <div class="platform-head">
            <span class="platform-tag">Android</span>
            <span class="platform-version">{escape(version)}</span>
          </div>
          <h2>Android 下载</h2>
          <p>点击后直接下载 APK。首次安装时，按系统提示允许浏览器或文件管理器安装未知应用。</p>
          <a class="button secondary" href="{escape(url)}">下载 APK</a>
          <p class="meta-line">包名：<code>{escape(package_id)}</code></p>
{render_link_line("下载地址", url)}
        </article>
    """


def main() -> None:
    args = parse_args()
    cards: list[str] = []
    if args.ios_url and args.ios_version and args.ios_bundle_id:
        cards.append(render_ios(args.ios_url, args.ios_version, args.ios_bundle_id))
    if args.testflight_url and args.testflight_version and args.testflight_bundle_id:
        cards.append(
            render_testflight(
                args.testflight_url,
                args.testflight_version,
                args.testflight_bundle_id,
            )
        )
    if args.android_url and args.android_version and args.android_package_id:
        cards.append(
            render_android(
                args.android_url,
                args.android_version,
                args.android_package_id,
            )
        )
    if not cards:
        raise SystemExit("at least one platform must be provided")

    body = "\n".join(cards)
    html_text = f"""<!doctype html>
<html lang="zh-CN">
  <head>
    <meta charset="utf-8">
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <title>MobileVC 安装</title>
    <style>
      :root {{
        --bg: #050505;
        --panel: rgba(11, 11, 11, 0.92);
        --line: rgba(255, 255, 255, 0.1);
        --text: #ffffff;
        --muted: #d4d4d8;
        --soft: #a1a1aa;
        --brand: #fdba74;
        --brand-2: #fb923c;
      }}
      * {{ box-sizing: border-box; }}
      body {{
        margin: 0;
        font-family: "PingFang SC", "Helvetica Neue", Helvetica, Arial, sans-serif;
        color: var(--text);
        background:
          radial-gradient(circle at top left, rgba(255, 188, 117, 0.17), transparent 22%),
          radial-gradient(circle at bottom right, rgba(255, 118, 38, 0.12), transparent 24%),
          linear-gradient(180deg, rgba(0, 0, 0, 0.30), rgba(0, 0, 0, 0.62) 45%, rgba(0, 0, 0, 0.88));
        min-height: 100vh;
      }}
      main {{
        min-height: 100vh;
        display: grid;
        place-items: center;
        padding: 24px;
      }}
      .card {{
        width: min(900px, 100%);
        overflow: hidden;
        border: 1px solid var(--line);
        border-radius: 32px;
        background: var(--panel);
        box-shadow: 0 24px 75px rgba(0, 0, 0, 0.55);
      }}
      .head {{
        display: flex;
        align-items: center;
        justify-content: space-between;
        border-bottom: 1px solid var(--line);
        padding: 18px 24px;
      }}
      .head small {{
        color: var(--soft);
        font-size: 12px;
        font-weight: 700;
        letter-spacing: 0.26em;
        text-transform: uppercase;
      }}
      .dots {{
        display: flex;
        gap: 8px;
      }}
      .dot {{
        width: 12px;
        height: 12px;
        border-radius: 999px;
      }}
      .dot.red {{ background: rgba(248, 113, 113, 0.85); }}
      .dot.yellow {{ background: rgba(250, 204, 21, 0.85); }}
      .dot.green {{ background: rgba(74, 222, 128, 0.85); }}
      .body {{
        padding: 28px;
      }}
      .eyebrow {{
        display: inline-flex;
        align-items: center;
        gap: 8px;
        padding: 4px 12px;
        border: 1px solid rgba(253, 186, 116, 0.2);
        border-radius: 999px;
        background: rgba(253, 186, 116, 0.1);
        color: #fed7aa;
        font-size: 12px;
        font-weight: 700;
        letter-spacing: 0.24em;
        text-transform: uppercase;
      }}
      h1 {{
        margin: 16px 0 12px;
        font-size: 34px;
        line-height: 1.06;
        font-weight: 900;
        letter-spacing: -0.04em;
      }}
      .lead {{
        margin: 0 0 24px;
        line-height: 1.7;
        color: var(--muted);
      }}
      .grid {{
        display: grid;
        gap: 18px;
        grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
      }}
      .platform-card {{
        border: 1px solid var(--line);
        border-radius: 24px;
        padding: 20px;
        background: rgba(255, 255, 255, 0.03);
      }}
      .platform-head {{
        display: flex;
        align-items: center;
        justify-content: space-between;
        gap: 12px;
        margin-bottom: 12px;
      }}
      .platform-tag {{
        display: inline-flex;
        align-items: center;
        padding: 4px 10px;
        border-radius: 999px;
        background: rgba(253, 186, 116, 0.1);
        border: 1px solid rgba(253, 186, 116, 0.2);
        color: #fed7aa;
        font-size: 12px;
        font-weight: 700;
        letter-spacing: 0.18em;
        text-transform: uppercase;
      }}
      .platform-tag.testflight {{
        background: rgba(255, 255, 255, 0.08);
        border-color: rgba(255, 255, 255, 0.12);
        color: #ffffff;
      }}
      .platform-version {{
        color: var(--soft);
        font-size: 13px;
      }}
      h2 {{
        margin: 0 0 10px;
        font-size: 24px;
      }}
      p {{
        margin: 0 0 12px;
        line-height: 1.7;
        color: var(--muted);
      }}
      .button {{
        display: inline-flex;
        align-items: center;
        justify-content: center;
        gap: 8px;
        margin: 10px 0 14px;
        padding: 14px 22px;
        border-radius: 18px;
        border: 1px solid rgba(253, 186, 116, 0.24);
        background: rgba(253, 186, 116, 0.12);
        color: #fed7aa;
        text-decoration: none;
        font-weight: 700;
      }}
      .button.secondary {{
        border-color: rgba(255, 255, 255, 0.12);
        background: rgba(255, 255, 255, 0.06);
        color: #ffffff;
      }}
      .meta-line {{
        margin: 0;
        color: var(--soft);
        font-size: 14px;
      }}
      .link-line {{
        margin: 12px 0 0;
        display: grid;
        gap: 6px;
      }}
      .link-label {{
        color: var(--soft);
        font-size: 12px;
        letter-spacing: 0.12em;
        text-transform: uppercase;
      }}
      .link-text {{
        color: #fdba74;
        text-decoration: none;
        word-break: break-all;
        line-height: 1.6;
        font-size: 13px;
      }}
      .link-text:hover {{
        text-decoration: underline;
      }}
      code {{
        padding: 2px 6px;
        border-radius: 8px;
        background: rgba(255, 255, 255, 0.08);
        color: #fff;
        font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
      }}
    </style>
  </head>
  <body>
    <main>
      <section class="card">
        <div class="head">
          <small>MobileVC Install</small>
          <div class="dots">
            <span class="dot red"></span>
            <span class="dot yellow"></span>
            <span class="dot green"></span>
          </div>
        </div>
        <div class="body">
          <div class="eyebrow">MobileVC</div>
          <h1>选择安装方式</h1>
          <p class="lead">iPhone 请用 Safari 安装，Android 可直接下载 APK。安装完成后，再回到电脑端 `mobilevc start` 输出的连接信息中进行扫码或手动连接。</p>
          <div class="grid">
{body}
          </div>
        </div>
      </section>
    </main>
  </body>
</html>
"""
    Path(args.output).write_text(html_text, encoding="utf-8")


if __name__ == "__main__":
    main()
