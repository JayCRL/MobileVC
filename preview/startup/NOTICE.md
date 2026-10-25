# dsh-startup-screen runtime (adapted)

`preview/startup/splash.js` and `splash.css` are adapted from
[`QIFANAI/dsh-startup-screen`](https://github.com/QIFANAI/dsh-startup-screen)
v1.6.0 (MIT), with asset/script paths changed to MobileVC's `/mobilevc/startup/`
prefix. The upstream project is a DSH plugin; MobileVC bundles only the
browser runtime so the splash works without depending on the plugin's host
route or DSH web auth.

No audio assets are bundled. Missing audio falls back to the upstream runtime's
TTS/WebAudio fallback. The DeepSeek whale mark embedded in the runtime belongs
to its original rights holders; this is a community integration, not an
official DeepSeek product.
