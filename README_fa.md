# SNISPF Core (Go) - راهنمای فارسی

SNISPF یک هسته ترمینالی برای عبور از DPI است که به صورت headless اجرا می شود و بین کلاینت شما و سرور upstream قرار می گیرد.

این هسته بر اساس روش [patterniha's SNI-Spoofing](https://github.com/patterniha/SNI-Spoofing) ساخته شده است و اعتبار ایده اصلی و روش پیاده سازی به طور کامل متعلق به [@patterniha](https://github.com/patterniha) است.

راهنمای انگلیسی: `README.md`

## این هسته چه کاری انجام می دهد

SNISPF مسیر ارتباط را این طور مدیریت می کند:

1. کلاینت شما به listener محلی وصل می شود (`LISTEN_HOST:LISTEN_PORT`).
2. SNISPF به مقصد upstream وصل می شود (`CONNECT_IP:CONNECT_PORT`).
3. SNISPF یکی از روش های bypass را اعمال می کند (`fragment`، `fake_sni`، `combined` یا `wrong_seq`).

## پیش نیازها (خیلی مهم)

| پلتفرم | روش های معمول (`fragment`/`fake_sni`/`combined`) | روش `wrong_seq` |
|---|---|---|
| Linux | بدون دسترسی خاص هم کار می کند | نیازمند `root` یا `CAP_NET_RAW` |
| Windows | در حالت عادی قابل اجرا است | نیازمند Administrator + فایل های `WinDivert.dll` و `WinDivert64.sys` |
| OpenWrt | قابل اجرا است | نیازمند `CAP_NET_RAW`/root و پشتیبانی AF_PACKET |

برای بررسی قابلیت های محیط اجرا:

```powershell
.\snispf.exe --info
```

`--info` مستقل از config است و به `--config` یا `config.json` نیاز ندارد.

اگر raw injection در دسترس نباشد، خروجی `--info` می تواند مقدار `raw_injection_diagnostic=...` را نشان دهد.

## شروع سریع (۴ مرحله)

### مرحله ۱) ساخت باینری

```powershell
go build -o snispf.exe ./cmd/snispf
```

### مرحله ۲) ساخت و اعتبارسنجی config

```powershell
.\snispf.exe --generate-config .\config.json
.\snispf.exe --config .\config.json --config-doctor
```

### مرحله ۳) تنظیم یک پروفایل امن پایه

پروفایل پیشنهادی برای شروع (پایدارتر):

```json
{
  "LISTEN_HOST": "127.0.0.1",
  "LISTEN_PORT": 40443,
  "LOG_LEVEL": "info",
  "CONNECT_IP": "188.114.98.0",
  "CONNECT_PORT": 443,
  "FAKE_SNI": "auth.vercel.com",
  "BYPASS_METHOD": "fragment"
}
```

توضیح فیلدها:

| فیلد | معنی |
|---|---|
| `LISTEN_HOST:LISTEN_PORT` | آدرس/پورت محلی که کلاینت باید به آن وصل شود |
| `LOG_LEVEL` | سطح لاگ: `error`، `warn`، `info`، `debug` |
| `CONNECT_IP:CONNECT_PORT` | مقصد upstream که SNISPF به آن وصل می شود |
| `FAKE_SNI` | دامنه SNI برای روش های fake/combined |
| `BYPASS_METHOD` | روش bypass (`fragment`، `fake_sni`، `combined`، `wrong_seq`) |

نکته تقدم تنظیمات:

- اگر `ENDPOINTS` تعریف شده باشد، مقادیر واقعی اتصال از `ENDPOINTS` خوانده می شود.
- فیلدهای top-level یعنی `CONNECT_IP`، `CONNECT_PORT` و `FAKE_SNI` برای سازگاری با نسخه های قبلی حفظ شده اند.
- اگر این فیلدها با `ENDPOINTS[0]` اختلاف داشته باشند، هنگام startup یک warning لاگ می شود که override شدن توسط `ENDPOINTS[0]` را مشخص می کند.

### مرحله ۴) اجرا و اتصال کلاینت

```powershell
.\snispf.exe --config .\config.json
```

در کلاینت خود تنظیم کنید:

- Address: `127.0.0.1`
- Port: `40443` (یا `LISTEN_PORT` شما)

بقیه تنظیمات پروتکل کلاینت را تغییر ندهید.

## انتخاب روش bypass

ترتیب پیشنهادی:

1. `fragment` برای اولین تست
2. در صورت نیاز `fake_sni` یا `combined`
3. `wrong_seq` فقط وقتی پیش نیازها کامل است

پیش نیازها و محدودیت های `wrong_seq`:

1. دقیقا یک endpoint فعال داشته باشید.
2. raw injection روی سیستم فعال باشد.
3. طول SNI حداکثر `219` بایت باشد.
4. اندازه fake ClientHello حداکثر `1460` بایت باشد.
5. در صورت نیاز timeout را با `WRONG_SEQ_CONFIRM_TIMEOUT_MS` تنظیم کنید (پیش فرض `2000`).
6. در تغییر مسیر multi-WAN/multi-WLAN ممکن است برای rebind شدن raw injector نیاز به restart باشد.

نکته عملی برای multi-WAN:

- `wrong_seq` حالت strict است و برای یک مسیر upstream پایدار بهتر کار می کند.
- برای سازگاری خودکار per-connection با تغییر مسیر WAN، `fragment` یا `combined` انتخاب بهتری است.

## حالت های اجرا

### حالت مستقیم (ساده ترین)

```powershell
.\snispf.exe --config .\config.json
```

نمونه override با فلگ:

```powershell
.\snispf.exe --config .\config.json --listen 0.0.0.0:40443 --connect 188.114.98.0:443 --sni auth.vercel.com --method combined
```

### حالت Service API (برای UI و اتوماسیون)

```powershell
.\snispf.exe --service --service-addr 127.0.0.1:8797
```

با توکن:

```powershell
.\snispf.exe --service --service-addr 127.0.0.1:8797 --service-token your-token
```

## API سریع

آدرس پایه پیش فرض: `http://127.0.0.1:8797`

- `GET /v1/status`
- `POST /v1/start`
- `POST /v1/stop`
- `GET /v1/health`
- `GET /v1/validate`
- `GET /v1/logs?limit=300&level=ALL`

اگر توکن فعال باشد، هدر `X-SNISPF-Token` را بفرستید.

ترتیب پیشنهادی برای عیب یابی:

1. `/v1/status`
2. `/v1/validate`
3. `/v1/health`
4. `/v1/logs`

`/v1/health` شامل شمارنده های `wrong_seq` نیز هست:

- `confirmed`
- `timeout`
- `failed`
- `not_registered`
- `first_write_fail`

قرارداد کامل API در `docs/api-contract.md` قرار دارد.

## OpenWrt (جریان پیشنهادی)

ساخت باینری های OpenWrt:

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\build_openwrt_matrix.ps1
```

کپی روی روتر:

```bash
scp ./release/openwrt/snispf_openwrt_armv7 root@192.168.1.1:/tmp/
scp ./config.json root@192.168.1.1:/tmp/snispf_config.json
scp ./release/openwrt/openwrt_snispf.sh root@192.168.1.1:/tmp/
```

نصب روی روتر:

```sh
ssh root@192.168.1.1
chmod +x /tmp/openwrt_snispf.sh
ash /tmp/openwrt_snispf.sh install --binary /tmp/snispf_openwrt_armv7 --config /tmp/snispf_config.json
```

رفتار پیش فرض installer:

- بعد از نصب/استارت یک delayed restart یک بار انجام می دهد (پیش فرض `20s`).
- در حالت تعاملی درباره نصب watchdog سوال می پرسد (`--watchdog ask`).
- در حالت غیرتعاملی، `ask` به صورت خودکار مثل نصب خودکار عمل می کند.

پیش فرض watchdog و تنظیم آن:

- زمان بندی پیش فرض هر `1` دقیقه است.
- در حالت process down، باز نبودن پورت listen و الگوهای degraded مربوط به raw injector سرویس را restart می کند.

نصب اجباری watchdog یا تنظیم delayed restart:

```sh
ash /tmp/openwrt_snispf.sh watchdog-install
ash /tmp/openwrt_snispf.sh install --binary /tmp/snispf_openwrt_armv7 --config /tmp/snispf_config.json --watchdog auto --post-restart-delay 20
```

دستورهای مهم مدیریت:

```sh
ash /tmp/openwrt_snispf.sh status
ash /tmp/openwrt_snispf.sh logs --follow
ash /tmp/openwrt_snispf.sh monitor --watch 30 --interval 2
ash /tmp/openwrt_snispf.sh doctor
```

برای `wrong_seq` در OpenWrt می توانید capability بدهید:

```sh
setcap cap_net_raw+ep /path/to/snispf_openwrt_armv7
```

## Build/Release اسکریپت ها

- Windows amd64: `powershell -ExecutionPolicy Bypass -File .\scripts\build_windows_amd64.ps1`
- Linux amd64 (PowerShell): `powershell -ExecutionPolicy Bypass -File .\scripts\build_linux_amd64.ps1`
- Linux amd64 (bash): `bash ./scripts/build_linux_amd64.sh`
- Release matrix: `powershell -ExecutionPolicy Bypass -File .\scripts\build_release_matrix.ps1`
- OpenWrt matrix (PowerShell): `powershell -ExecutionPolicy Bypass -File .\scripts\build_openwrt_matrix.ps1`
- OpenWrt matrix (bash): `bash ./scripts/build_openwrt_matrix.sh`
- Verify (PowerShell): `powershell -ExecutionPolicy Bypass -File .\scripts\verify_release.ps1`
- Verify (bash): `bash ./scripts/verify_release.sh`

خروجی ها:

- Core: `release/` + `release/checksums.txt` + `release/release_manifest.json`
- OpenWrt: `release/openwrt/` + `release/openwrt/checksums.txt` + `release/openwrt/release_manifest.json`

## چک لیست عیب یابی

1. `--config-doctor` را اجرا کنید و خطاها را اصلاح کنید.
2. مطمئن شوید کلاینت به listener محلی SNISPF وصل می شود.
3. رسیدن به upstream را با `/v1/health` یا لاگ های startup چک کنید.
4. برای `wrong_seq` دسترسی های لازم و تک-endpoint بودن را بررسی کنید.
5. خروجی `/v1/logs` را برای `timeout`، `failed` و `not_registered` چک کنید.

## نقشه مستندات

- `docs/README.md`
- `docs/beginner-guide.md`
- `docs/api-contract.md`
- `docs/internals.md`
- `docs/examples.md`
- `docs/roadmap.md`
