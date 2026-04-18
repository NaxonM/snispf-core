# SNISPF Core

<div dir="rtl">

هسته‌ی دور زدن DPI برای Go، طراحی‌شده برای اجرا در محیط ترمینال به‌صورت headless، به‌عنوان یک پروکسی TCP محلی پایدار بین کلاینت شما و اندپوینت مقصد.

پیاده‌سازی تکنیک [SNI-Spoofing توسط @patterniha](https://github.com/patterniha/SNI-Spoofing). تمام اعتبار ایده و روش اصلی متعلق به [@patterniha](https://github.com/patterniha) است.

راهنمای انگلیسی: `README.md`

---

## نحوه کارکرد

</div>

```
کلاینت شما  →  SNISPF (127.0.0.1:LISTEN_PORT)  →  اندپوینت مقصد (CONNECT_IP:CONNECT_PORT)
```

<div dir="rtl">

SNISPF پیام TLS ClientHello خروجی را رهگیری کرده و پیش از ارسال، استراتژی دور زدن را روی آن اعمال می‌کند. تنظیمات کلاینت شما بدون تغییر باقی می‌ماند — تمام منطق دور زدن درون SNISPF است.

---

## شروع سریع

### ۱. ساخت (Build)

</div>

```bash
go build -o snispf.exe ./cmd/snispf
```

<div dir="rtl">

### ۲. ساخت و اعتبارسنجی فایل کانفیگ

</div>

```bash
.\snispf.exe --generate-config .\config.json
.\snispf.exe --config .\config.json --config-doctor
```

<div dir="rtl">

قبل از ادامه، تمام خطاهای گزارش‌شده توسط config-doctor را برطرف کنید.

### ۳. اجرا

> **`wrong_seq` نیاز به ترمینال با دسترسی بالا دارد.** در ویندوز، به‌عنوان Administrator اجرا کنید. در لینوکس، به‌عنوان root اجرا کنید یا ابتدا `CAP_NET_RAW` را اعطا کنید. جزئیات را در [استراتژی‌های دور زدن](#استراتژیهای-دور-زدن) ببینید.

</div>

```bash
.\snispf.exe --config .\config.json
```

<div dir="rtl">

### ۴. کلاینت خود را تنظیم کنید

</div>

```
Address: 127.0.0.1
Port:    40443  (یا مقدار LISTEN_PORT تنظیم‌شده در کانفیگ)
```

<div dir="rtl">

سایر تنظیمات پروتکل کلاینت را بدون تغییر نگه دارید.

---

## کانفیگ پیشنهادی

</div>

```json
{
  "LISTEN_HOST": "127.0.0.1",
  "LISTEN_PORT": 40443,
  "LOG_LEVEL": "info",
  "CONNECT_IP": "203.0.113.10",
  "CONNECT_PORT": 443,
  "FAKE_SNI": "edge-a.example.com",
  "BYPASS_METHOD": "wrong_seq",
  "FRAGMENT_STRATEGY": "sni_split",
  "FRAGMENT_DELAY": 0.05,
  "USE_TTL_TRICK": false,
  "FAKE_SNI_METHOD": "raw_inject",
  "WRONG_SEQ_CONFIRM_TIMEOUT_MS": 2000,
  "ENDPOINTS": [
    {
      "NAME": "strict-primary",
      "IP": "203.0.113.10",
      "PORT": 443,
      "SNI": "edge-a.example.com",
      "ENABLED": true
    }
  ],
  "LOAD_BALANCE": "failover",
  "ENDPOINT_PROBE": true,
  "AUTO_FAILOVER": false,
  "FAILOVER_RETRIES": 0,
  "PROBE_TIMEOUT_MS": 2500
}
```

<div dir="rtl">

| فیلد | توضیح |
|---|---|
| `LISTEN_HOST:LISTEN_PORT` | آدرس محلی که کلاینت شما به آن متصل می‌شود |
| `CONNECT_IP:CONNECT_PORT` | مقصد upstream که SNISPF به آن متصل می‌شود |
| `FAKE_SNI` | SNI مورد استفاده در استراتژی‌های `fake_sni` و `combined` |
| `BYPASS_METHOD` | استراتژی: `fragment`، `fake_sni`، `combined`، یا `wrong_seq` |
| `FRAGMENT_STRATEGY` | نحوه تقسیم ClientHello (مثلاً `sni_split`) |
| `FRAGMENT_DELAY` | تأخیر بین فرگمنت‌ها بر حسب ثانیه |
| `USE_TTL_TRICK` | ارسال ClientHello جعلی با TTL پایین پیش از ClientHello واقعی |
| `FAKE_SNI_METHOD` | روش SNI جعلی: `raw_inject`، `prefix_fake` و غیره |
| `WRONG_SEQ_CONFIRM_TIMEOUT_MS` | پنجره تأیید برای حالت `wrong_seq` (پیش‌فرض: ۲۰۰۰) |
| `LOAD_BALANCE` | انتخاب اندپوینت: `round_robin`، `random`، `failover` |
| `ENDPOINT_PROBE` | حذف اندپوینت‌های غیرقابل‌دسترس در هنگام راه‌اندازی |
| `AUTO_FAILOVER` | تلاش مجدد در صورت خطای اتصال |
| `FAILOVER_RETRIES` | تعداد تلاش‌های failover |
| `PROBE_TIMEOUT_MS` | تایم‌اوت probe اندپوینت بر حسب میلی‌ثانیه |
| `LOG_LEVEL` | سطح لاگ: `error`، `warn`، `info`، `debug` |

**اولویت‌بندی کانفیگ:** اگر `ENDPOINTS` تعریف شده باشد، مقادیر اتصال از آن خوانده می‌شوند. فیلدهای سطح بالا (`CONNECT_IP`، `CONNECT_PORT`، `FAKE_SNI`) برای سازگاری با نسخه‌های قبلی باقی مانده‌اند. در صورت تعارض `ENDPOINTS[0]` با فیلدهای سطح بالا، یک هشدار در لاگ راه‌اندازی ثبت می‌شود.

---

## استراتژی‌های دور زدن

`wrong_seq` استراتژی پیشنهادی پیش‌فرض است. مؤثرترین روش دور زدن را ارائه می‌دهد، مشروط بر اینکه پیش‌نیازهای پلتفرم برآورده شده باشند. تنها در صورت عدم امکان، به استراتژی‌های ساده‌تر بازگردید.

| استراتژی | نیاز به دسترسی بالا | توصیه‌شده برای |
|---|---|---|
| **`wrong_seq`** | بله — جدول زیر را ببینید | انتخاب پیش‌فرض هنگام برآورده‌بودن پیش‌نیازها |
| `combined` | خیر (با degradation graceful) | زمانی که raw injection در دسترس نیست اما bypass قوی‌تر نیاز است |
| `fake_sni` | خیر (با degradation graceful) | جایگزین سبک‌تر `combined` |
| `fragment` | خیر | عیب‌یابی، محیط‌های محدود، یا آخرین راه‌حل |

### پیش‌نیازهای پلتفرم و دسترسی

| پلتفرم | `fragment`، `fake_sni`، `combined` | `wrong_seq` |
|---|---|---|
| Linux | بدون دسترسی خاص | **ترمینال privileged** — `root` یا `CAP_NET_RAW` |
| Windows | عادی | **ترمینال Administrator** + `WinDivert.dll` + `WinDivert64.sys` در همان پوشه‌ی فایل اجرایی |
| OpenWrt | عادی | **Privileged** — `CAP_NET_RAW` یا root + پشتیبانی از AF_PACKET |

> **ویندوز:** بسته release ویندوز شامل `WinDivert.dll` و `WinDivert64.sys` است. این فایل‌ها را در همان پوشه‌ی `snispf.exe` قرار دهید و از ترمینال Administrator اجرا کنید. بدون هر دو فایل، `wrong_seq` راه‌اندازی نمی‌شود و `--info` دلیل آن را گزارش می‌دهد.

> **لینوکس:** یا به‌عنوان root اجرا کنید، یا یک‌بار capability اعطا کنید: `sudo setcap cap_net_raw+ep ./snispf`

محدودیت‌های اضافی `wrong_seq`:
- دقیقاً یک اندپوینت فعال
- طول SNI ≤ ۲۱۹ بایت
- اندازه ClientHello جعلی ≤ ۱۴۶۰ بایت (هر دو توسط `--config-doctor` بررسی می‌شوند)

برای بررسی قابلیت‌های runtime پلتفرم: `.\snispf.exe --info` — این فلگ مستقل از کانفیگ است.

---

## حالت‌های اجرا

### حالت مستقیم (Direct mode)

</div>

```bash
.\snispf.exe --config .\config.json
```

<div dir="rtl">

override یک‌بار مصرف فلگ‌ها (در کانفیگ ذخیره نمی‌شوند):

</div>

```bash
.\snispf.exe --config .\config.json --listen 0.0.0.0:40443 --connect 188.114.98.0:443 --sni auth.vercel.com --method combined
```

<div dir="rtl">

### حالت Service API

یک API کنترلی HTTP برای اپ‌های دسکتاپ، launcher‌ها و اتوماسیون در معرض می‌گذارد.

</div>

```bash
.\snispf.exe --service --service-addr 127.0.0.1:8797
.\snispf.exe --service --service-addr 127.0.0.1:8797 --service-token your-token
```

<div dir="rtl">

آدرس پایه API: `http://127.0.0.1:8797`

| اندپوینت | متد | توضیح |
|---|---|---|
| `/v1/status` | GET | وضعیت worker، PID، زمان شروع |
| `/v1/start` | POST | اعتبارسنجی کانفیگ و راه‌اندازی worker |
| `/v1/stop` | POST | توقف worker |
| `/v1/health` | GET | TCP probe اندپوینت + شمارنده‌های `wrong_seq` |
| `/v1/validate` | GET | نتایج config doctor |
| `/v1/logs` | GET | tail لاگ (`?limit=300&level=ALL`) |

در صورت تنظیم token، هدر `X-SNISPF-Token: <token>` را با هر درخواست ارسال کنید.

ترتیب پیشنهادی عیب‌یابی: `/v1/status` ← `/v1/validate` ← `/v1/health` ← `/v1/logs`

مشخصات کامل درخواست/پاسخ: [`docs/api-contract.md`](docs/api-contract.md)

---

## حالت چند listener

اجرای چند listener محلی در یک پروسه:

</div>

```json
{
  "BYPASS_METHOD": "wrong_seq",
  "LISTENERS": [
    {
      "NAME": "edge-a",
      "LISTEN_HOST": "127.0.0.1",
      "LISTEN_PORT": 40443,
      "CONNECT_IP": "104.19.229.21",
      "CONNECT_PORT": 443,
      "FAKE_SNI": "hcaptcha.com"
    },
    {
      "NAME": "edge-b",
      "LISTEN_HOST": "127.0.0.1",
      "LISTEN_PORT": 40444,
      "CONNECT_IP": "104.19.229.22",
      "CONNECT_PORT": 443,
      "FAKE_SNI": "hcaptcha.com",
      "BYPASS_METHOD": "fragment"
    }
  ]
}
```

<div dir="rtl">

هر listener به‌طور مستقل اجرا می‌شود. وقتی `LISTENERS` تعریف شده باشد، مقادیر هر listener بر مقادیر پیش‌فرض سطح بالا اولویت دارند.

---

## استقرار سرویس لینوکس

بسته‌های لینوکس شامل template سیستمد و اسکریپت نصب هستند:

</div>

```bash
sudo bash ./install_linux_service.sh install --binary ./snispf_linux_amd64 --config ./config.json
sudo bash ./install_linux_service.sh status
sudo bash ./install_linux_service.sh restart
sudo bash ./install_linux_service.sh logs --lines 120
```

<div dir="rtl">

یونیت پیش‌فرض مقادیر `Restart=always` و `LimitNOFILE=65535` را تنظیم می‌کند.

---

## استقرار روی OpenWrt

ساخت و کپی بسته معماری مورد نظر به روتر:

</div>

```bash
powershell -ExecutionPolicy Bypass -File .\scripts\build_openwrt_matrix.ps1
scp ./release/openwrt/snispf_openwrt_x86_64_bundle.tar.gz root@192.168.1.1:/tmp/
```

<div dir="rtl">

نصب روی روتر:

</div>

```sh
ssh root@192.168.1.1
cd /tmp && tar -xzf snispf_openwrt_x86_64_bundle.tar.gz && cd snispf_openwrt_bundle
ash ./openwrt_snispf.sh install --binary ./snispf --config ./config.json
```

<div dir="rtl">

بسته شامل فایل اجرایی، `openwrt_snispf.sh`، و یک کانفیگ پیش‌فرض است.

**Watchdog:** به‌صورت تعاملی نصب می‌شود (هر ۱ دقیقه). در صورت خرابی پروسه، نبود پورت listen، یا الگوهای تخریب‌شده در لاگ raw-injector، سرویس را ریستارت می‌کند. نصب اجباری یا تنظیم:

</div>

```sh
ash ./openwrt_snispf.sh watchdog-install
ash ./openwrt_snispf.sh install --binary ./snispf --config ./config.json --watchdog auto --post-restart-delay 20
```

<div dir="rtl">

عملیات مفید:

</div>

```sh
ash ./openwrt_snispf.sh status
ash ./openwrt_snispf.sh logs --follow
ash ./openwrt_snispf.sh monitor --watch 30 --interval 2
ash ./openwrt_snispf.sh doctor
```

<div dir="rtl">

برای `wrong_seq` روی OpenWrt:

</div>

```sh
setcap cap_net_raw+ep /path/to/snispf
```

<div dir="rtl">

> **توجه:** اگر لاگ‌ها `socket: too many open files` نشان داد، با آخرین نسخه `openwrt_snispf.sh` مجدداً نصب کنید تا محدودیت `nofile` در procd اعمال شود.

---

## ساخت و انتشار

### ساخت محلی

</div>

```bash
go build -o snispf.exe ./cmd/snispf
```

<div dir="rtl">

### اسکریپت‌های cross-build

| هدف | دستور |
|---|---|
| Windows amd64 | `powershell -ExecutionPolicy Bypass -File .\scripts\build_windows_amd64.ps1` |
| Linux amd64 (PowerShell) | `powershell -ExecutionPolicy Bypass -File .\scripts\build_linux_amd64.ps1` |
| Linux amd64 (bash) | `bash ./scripts/build_linux_amd64.sh` |
| ماتریس release کامل | `powershell -ExecutionPolicy Bypass -File .\scripts\build_release_matrix.ps1` |
| ماتریس OpenWrt (PowerShell) | `powershell -ExecutionPolicy Bypass -File .\scripts\build_openwrt_matrix.ps1` |
| ماتریس OpenWrt (bash) | `bash ./scripts/build_openwrt_matrix.sh` |

تأیید صحت:

</div>

```bash
powershell -ExecutionPolicy Bypass -File .\scripts\verify_release.ps1
bash ./scripts/verify_release.sh
```

<div dir="rtl">

### خروجی‌های release

- فایل‌های اجرایی: `release/snispf_windows_amd64.exe`، `release/snispf_linux_amd64`، `release/snispf_linux_arm64`
- بسته‌ها: `release/snispf_*_bundle.{zip,tar.gz}`
- OpenWrt: `release/openwrt/` — فایل‌های اجرایی و بسته‌های per-arch، `openwrt_snispf.sh`، `openwrt_default_config.json`
- متادیتا: `release/checksums.txt`، `release/release_manifest.json`

معماری‌های ماتریس OpenWrt: `armv7`، `armv6`، `mipsle_softfloat`، `mips_softfloat`، `arm64`، `x86_64`

### GitHub Actions

Workflow: `.github/workflows/release.yml`

- `workflow_dispatch` — ساخت‌های draft/test
- Push tag (مثلاً `v1.2.3`) — release کامل با checksum و manifest

---

## مرجع CLI

</div>

```
--config <path>           بارگذاری فایل کانفیگ
--generate-config <path>  نوشتن کانفیگ پیش‌فرض در مسیر مشخص‌شده
--config-doctor           اعتبارسنجی کانفیگ و خروج
--info                    نمایش قابلیت‌های runtime پلتفرم (بدون نیاز به کانفیگ)
--listen <host:port>      override آدرس listen
--connect <ip:port>       override آدرس upstream
--sni <hostname>          override مقدار SNI
--method <strategy>       override استراتژی دور زدن
--service                 اجرا در حالت Service API
--service-addr <host:port>
--service-token <token>
--build-info / --version
```

<div dir="rtl">

alias‌های سازگار با نسخه‌های قبلی: `snispf run`، `snispf service`، `snispf doctor`، `snispf build-info`

---

## چک‌لیست تأیید

</div>

```bash
go test ./...
go vet ./...
go build -o snispf.exe ./cmd/snispf
powershell -ExecutionPolicy Bypass -File .\scripts\build_linux_amd64.ps1
powershell -ExecutionPolicy Bypass -File .\scripts\build_release_matrix.ps1
powershell -ExecutionPolicy Bypass -File .\scripts\verify_release.ps1
powershell -ExecutionPolicy Bypass -File .\scripts\integration_service_lifecycle.ps1
```

---

<div dir="rtl">

## عیب‌یابی

۱. `--config-doctor` را اجرا کرده و تمام خطاها را برطرف کنید.
۲. تأیید کنید کلاینت به آدرس و پورت listener محلی SNISPF اشاره می‌کند.
۳. دسترسی به upstream را از طریق `/v1/health` یا لاگ راه‌اندازی تأیید کنید.
۴. برای `wrong_seq`: دسترسی پلتفرم و محدودیت single-endpoint را بررسی کنید.
۵. `/v1/logs` را برای نتایج `timeout`، `failed`، و `not_registered` بررسی کنید.

---

## مستندات

| سند | مخاطب |
|---|---|
| [`docs/beginner-guide.md`](docs/beginner-guide.md) | راه‌اندازی اولیه و عیب‌یابی |
| [`docs/api-contract.md`](docs/api-contract.md) | schema و سازگاری Service API |
| [`docs/internals.md`](docs/internals.md) | معماری، مسیرهای کد، مشارکت در توسعه |
| [`docs/examples.md`](docs/examples.md) | پروفایل‌های کانفیگ با توضیحات |
| [`docs/roadmap.md`](docs/roadmap.md) | مسیر آینده و اهداف خارج از محدوده |

</div>
