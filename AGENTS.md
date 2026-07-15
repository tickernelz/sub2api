# AGENTS.md — Panduan Maintainer Fork `tickernelz/sub2api`

Repo ini adalah **fork ringan** dari upstream `Wei-Shaw/sub2api`. Tujuan fork: mengikuti upstream sedekat mungkin, dengan **hanya sejumlah kecil kustomisasi yang sengaja di-keep**. Semua fitur fork lama (Kiro/OpenCode/Cursor/Grok-registry/multi-group/dll) **sudah dibuang** — jangan hidupkan kembali kecuali diminta eksplisit.

Baca dokumen ini sebelum melakukan sync/merge dengan upstream. Tujuannya: sync berikutnya harus cepat dan bersih, bukan perang konflik.

---

## 1. Identitas Repo & Remote

| Hal | Nilai |
|---|---|
| Remote `origin` | `git@github.com:tickernelz/sub2api.git` (fork kita) |
| Remote `wei-shaw` | `git@github.com:Wei-Shaw/sub2api.git` (upstream) |
| Go module path | `github.com/Wei-Shaw/sub2api` (**tetap ikut upstream**, jangan diganti ke `tickernelz`) |
| Branch utama | `main` |
| Versi dilacak di | `backend/cmd/server/VERSION` (di-drive oleh git tag, lihat §6) |

> ⚠️ **Module path penting:** karena kita full-rewrite di atas upstream, module path = `Wei-Shaw`. Kalau meng-cherry-pick commit fork lama yang pakai `tickernelz/sub2api`, **perbaiki import path-nya** ke `Wei-Shaw/sub2api` atau build akan gagal.

Pastikan remote upstream ada sebelum sync:
```bash
git remote get-url wei-shaw || git remote add wei-shaw git@github.com:Wei-Shaw/sub2api.git
git fetch wei-shaw
```

---

## 2. Fitur Kustom yang WAJIB di-KEEP

Ada **tiga fitur produk** yang di-keep, total 5 commit. Selain ini, ikuti upstream apa adanya.

> ⚠️ **SHA berubah tiap sync.** SHA aktif setelah sync 2026-07-15 di atas upstream `5b5e41450`: `023868d9a`, `3737cb7b2`, `6eea00754`, `264e9720c`, dan `a49090087`. Setiap reset + cherry-pick menghasilkan SHA baru; cari ulang berdasarkan judul commit dengan `git log --oneline --all --grep=...` (lihat §4).

### Fitur A: OpenAI/Codex OAuth jangan auto-disable saat refresh token gagal/reused

Akun OpenAI OAuth **tidak boleh** langsung di-`SetError`/unschedule ketika refresh token gagal karena `refresh_token_reused`. Alasannya: `refresh_token_reused` cuma menandakan refresh token sudah dikonsumsi/dirotasi — **bukan** bukti access token saat ini tidak valid. Akun tetap dibiarkan schedulable, dan UI menampilkan warning "reauth required".

Dua commit sumber (fork-only, tidak ada di upstream):

| Commit | Judul | Cakupan |
|---|---|---|
| `023868d9a` | `fix(openai): keep oauth accounts schedulable on reused refresh token` | Backend soft-handle |
| `3737cb7b2` | `feat(admin): show OpenAI OAuth reauth warning` | Frontend warning UI |

**File yang disentuh (referensi saat re-apply):**

Backend (`023868d9a`):
- `backend/internal/service/openai_refresh_token_state.go` *(file baru — inti logika)*
- `backend/internal/service/token_refresh_service.go` *(titik keputusan `isNonRetryableRefreshError`)*
- `backend/internal/service/openai_token_provider.go`
- `backend/internal/service/token_refresher.go`
- `backend/internal/handler/admin/account_handler.go`
- `backend/internal/repository/account_repo.go`
- `backend/internal/service/admin_account.go`
- `backend/internal/service/token_refresh_service_test.go`

Frontend (`3737cb7b2`):
- `frontend/src/components/admin/account/AccountActionMenu.vue` *(computed `hasOpenAIRefreshTokenReauthRequired`)*
- `frontend/src/views/admin/AccountsView.vue`
- `frontend/src/types/index.ts`
- `frontend/src/i18n/locales/en/admin/accounts.ts`, `frontend/src/i18n/locales/zh/admin/accounts.ts`
- `frontend/src/views/admin/__tests__/AccountsView.openaiReauthWarning.spec.ts`
- `Makefile`

**Titik konflik yang sudah diketahui saat re-apply:**
1. `token_refresh_service.go` cabang `if isNonRetryableRefreshError(err) {` — upstream pakai `logredact.RedactText(err.Error())`, commit fork pakai `fmt.Sprintf`. **Gabungkan:** pertahankan blok soft-handle (`shouldSoftHandleOpenAIRefreshTokenReused` → `markOpenAIRefreshTokenReused` → `ensureOpenAIPrivacy` → `return err`) **DAN** pakai `logredact.RedactText` dari upstream untuk `errorMsg`.
2. `AccountActionMenu.vue` — upstream punya `isShadow`/`isOpenAIOAuthParent`/`supportsPrivacy` versi shadow-aware. **Keep versi upstream** + tambahkan computed `hasOpenAIRefreshTokenReauthRequired`; jangan pakai `supportsPrivacy` versi lama dari commit fork.
3. **`token_refresh_service_test.go` DUPLIKAT (kambuh tiap sync!):** commit fork A nambah field `updateExtraCalls` + method `UpdateExtra` di mock `tokenRefreshAccountRepo`, TAPI upstream juga sudah punya keduanya (field `updateExtraCalls`, `lastExtraUpdates`, method `UpdateExtra`). Cherry-pick → **deklarasi ganda** → `redeclared`/`method already declared`. **Ini TAK kelihatan di `go build`/`go test ./...` polos — cuma muncul di `go test -tags=unit` (perintah CI `make test-unit`).** Ini yang bikin CI v0.1.157 merah. **Fix:** hapus field `updateExtraCalls` duplikat + method `UpdateExtra` versi fork, lalu lipat assignment `r.lastExtraUpdate = updates` (singular, dipakai test fork) ke DALAM method `UpdateExtra` upstream yang bertahan (yang set `lastExtraUpdates` plural). Fix ini sudah di-bake ke commit fitur A sejak sync #2, tapi kalau kambuh lagi ulangi pola ini.

### Fitur B: Stream stale-detection + auto-failover (watchdog) di semua provider

Watchdog aliran (stale-stream) yang di-wire ke SEMUA loop streaming provider (OpenAI chat/messages, Anthropic passthrough, Bedrock, Antigravity ×5). Mendeteksi 3 bentuk stall dan failover ke akun lain **sebelum** ada byte yang dikirim ke client:
- **TTFT timeout** — upstream connect tapi first event tak pernah datang → failover.
- **chunk-gap warning** — gap antar-event lunak (log/metrics only, tidak failover).
- **chunk-gap timeout** — gap antar-event keras → failover.

Desain inti (JANGAN copy-paste timer per-loop seperti fork lama):
- **Satu** `StreamWatchdog` (injectable clock utk test deterministik) + `StreamRetrySettings` berlapis (per-platform override > global > default), cache in-process 60s, semantik row-missing vs disabled dibedakan.
- Gap timer reset di **setiap** upstream event (`OnUpstreamEvent()`) → reasoning model tak salah-vonis stale.
- Failover dijaga `c.Writer.Written()` (via `streamOutputCommitted`): retry hanya bila belum ada output; kalau sudah, fail clean (tak ada output ganda).
- Komplementer dengan `StreamTimeoutSettings` yang sudah ada (itu governs post-timeout action).

Satu commit sumber (fork-only): `6eea00754` `feat(gateway): stream stale detection + auto-failover across all providers` (~1900 baris, 20 file). Default enabled (TTFT 60s, gap-warn 10s, gap-timeout 30s).

**File inti (port verbatim — file baru, 0 collision dengan upstream):**
- `backend/internal/service/stream_stale_watchdog.go` *(watchdog + injectable clock)*
- `backend/internal/service/stream_retry_settings.go` *(settings + resolver berlapis + Get/Set)*
- `backend/internal/service/stream_watchdog_integration.go` *(glue: cache, `newStreamWatchdogForPlatform`, `decideStreamStall`, metrics)*
- `+ stream_stale_watchdog_test.go`, `stream_retry_settings_test.go`

**Admin surface (additive):** `setting_handler_runtime.go` (3 handler: Get/Update StreamRetry + Metrics), `dto/settings.go`, `server/routes/admin.go` (3 route), `domain_constants.go` (`SettingKeyStreamRetrySettings`), frontend `api/admin/settings.ts` + `SettingsView.vue` + i18n `streamRetry` block.

> ⚠️ **PITFALL WIRING (pelajaran sync 2026-07):** upstream me-refactor besar gateway — `gateway_service.go` (7294→1289 baris) dipecah jadi `gateway_anthropic_passthrough.go`, `gateway_upstream_response.go`; antigravity dipecah jadi `antigravity_gateway_streaming.go` + `antigravity_gateway_upstream.go`. **10 titik wiring watchdog pindah file.** Jika cherry-pick mentah gagal atau upstream mengubah loop, **JANGAN merge body lama secara buta** — re-implement wiring ke lokasi loop upstream yang baru: (1) init watchdog setelah `intervalCh = intervalTicker.C`, (2) `staleWatchdog.OnUpstreamEvent()` setelah `line := ev.line`, (3) 3 select-arm (`staleTTFTCh`/`staleGapWarnCh`/`staleGapTimeoutCh`) sebelum `case <-keepaliveCh`. Setelah re-apply, audit total 10 init, 10 event-reset, dan 30 `decideStreamStall` call.

### Fitur C: Netralisasi harmony `<|channel|>` token supaya request tak kena upstream `invalid_prompt` block

OpenAI `/v1/responses` upstream punya **request-level hard guard** untuk harmony "hidden analysis channel" header. Ketika request body memuat literal ASCII `<|channel|>` yang **langsung diikuti** `analysis` (header chain-of-thought tersembunyi harmony; toleran spasi/newline), upstream menolak **seluruh request** dengan HTTP 200 stream-internal `response.failed` + `error.code=invalid_prompt` (message `"Request blocked."`). Ini guard anti-injection (mencegah spoofing channel reasoning tersembunyi), **bukan** content-moderation — `content_moderation` lokal mencatat `allowed=true`.

Dampak nyata: request sah yang kebetulan memuat literal itu (mis. Hermes me-review file kode/test yang berisi `<|channel|>analysis` sebagai fixture) ikut kena block, lalu client (Hermes) me-retry 10× dengan body identik → pasti gagal terus, buang menit.

**Perbaikan (fork-only):** sebelum body dikirim ke upstream, ganti dua ASCII pipe `|` (U+007C) di dalam `<|channel|>` jadi fullwidth pipe `｜` (U+FF5C) → `<｜channel｜>`. Terbukti empiris (gpt-5.6-sol, `/v1/responses` stream) varian ini **lolos** upstream, hampir tak terlihat beda oleh model/manusia (fixture text tetap terbaca), dan reversibel visual (bukan delete). Zero-width chars (U+200B dll) **tidak** menetralkan karena upstream strip zero-width sebelum matching. Hanya token `<|channel|>` yang disentuh; harmony token lain (`<|start|>`, `<|message|>`, `<|end|>`) dan `analysis` tidak diutak-atik.

Dua commit sumber (fork-only): `264e9720c` (neutralizer + dua builder) dan `a49090087` (observability `invalid_prompt`). Cari ulang dengan `git log --oneline --all --grep="harmony"` atau `--grep="invalid_prompt"`. Default **enabled** (`gateway.neutralize_harmony_channel_token=true`); bisa dimatikan tanpa rebuild.

**File inti (file baru — 0 collision dengan upstream):**
- `backend/internal/service/openai_harmony_channel_neutralize.go` *(helper `neutralizeOpenAIHarmonyChannelToken` + detektor `detectOpenAIInvalidPrompt`; byte-level, hot-path aman: `bytes.Contains` guard → nol alokasi saat tak ada token)*
- `+ openai_harmony_channel_neutralize_test.go` *(unit: neutralize glued/spaced/multi/no-op/idempotent/no-mutation + `detectOpenAIInvalidPrompt` flat/nested/case-insensitive/negatif)*
- `+ openai_harmony_channel_neutralize_forward_test.go` *(integrasi: kedua builder, flag ON/OFF, cfg nil = ON)*

**Titik wiring (2 builder HTTP `/v1/responses` — keduanya WAJIB, path berbeda):**
- `backend/internal/service/openai_gateway_forward.go` → `buildUpstreamRequest` (jalur rewrite), tepat sebelum `http.NewRequestWithContext(ctx, "POST", targetURL, ...)`.
- `backend/internal/service/openai_gateway_passthrough.go` → `buildUpstreamRequestOpenAIPassthrough` (jalur passthrough — **builder terpisah**, tidak lewat `buildUpstreamRequest`; ini yang dipakai akun `openai_passthrough`/`codex_responses`).

**Layer 2 — observability `invalid_prompt` (additive, bagian dari Fitur C):**
Saat block masih terjadi (mis. harmony guard upstream berubah ke token lain sehingga netralisasi meleset), block itu tidak boleh "hilang diam-diam" di monitoring. `detectOpenAIInvalidPrompt(payload)` (mengikuti struktur `detectOpenAICyberPolicy`) mendeteksi `error.code`/`response.error.code == "invalid_prompt"` pada event stream `response.failed`, lalu memanggil `recordOpenAIStreamUpstreamError(..., "invalid_prompt", ...)` supaya masuk `ops_error_logs` (kind=`invalid_prompt`). Di-wire di **dua** loop streaming `response.failed`, ditempatkan **setelah** cabang cyber + passthrough-rule + failover (yang semuanya `return`/`Mark` lebih dulu), dijaga flag `cyberMarked` supaya tidak double-record:
- `openai_gateway_passthrough.go` → `handleStreamingResponsePassthrough` (passthrough=true)
- `openai_gateway_response_handling.go` → loop `response.failed` jalur rewrite (passthrough=false)
Sifatnya murni observasi: tidak mengubah perilaku transfer/return/failover yang ada; hanya menambah 1 baris ops event kalau sebelumnya event `invalid_prompt` itu tidak tercatat.

Pola wiring identik di kedua tempat:
```go
if s.cfg == nil || s.cfg.Gateway.NeutralizeHarmonyChannelToken {
    if neutralized, changed := neutralizeOpenAIHarmonyChannelToken(body); changed {
        body = neutralized
        logger.LegacyPrintf("service.openai_gateway", "[OpenAI] Neutralized harmony <|channel|> token ... (account: %s)", account.Name)
    }
}
```

**Config surface (additive):**
- `backend/internal/config/config.go`: field `GatewayConfig.NeutralizeHarmonyChannelToken bool` (mapstructure `neutralize_harmony_channel_token`) + `viper.SetDefault("gateway.neutralize_harmony_channel_token", true)`.

**Titik konflik yang mungkin saat re-apply:**
1. **Jangan cukup wire satu builder.** Passthrough punya builder sendiri; kalau upstream me-refactor/memindah salah satu builder, pastikan **kedua** titik `http.NewRequestWithContext(... "POST" ...)` untuk `/v1/responses` tetap ter-cover. Audit: `grep -n 'NewRequestWithContext' internal/service/openai_gateway_forward.go internal/service/openai_gateway_passthrough.go`.
2. **Guard `s.cfg == nil`** wajib dipertahankan supaya test tanpa cfg (dan default) berperilaku ON — konsisten dengan viper default `true`.
3. **Kalau upstream sudah punya penanganan setara** (mis. upstream menambah sanitizer/neutralizer harmony sendiri, atau upstream tak lagi kena block ini), **fitur ini gugur** — jangan re-apply; verifikasi dulu dengan repro block di §2-C di bawah, lalu hapus Fitur C dari daftar ini.

**Repro/verifikasi cepat (opsional, butuh 1 request berbayar ke upstream):** kirim `/v1/responses` dengan `input` memuat `<|channel|>analysis`. Sebelum fix → `invalid_prompt`/`Request blocked`. Sesudah fix → lolos normal. Unit+integration test sudah mengunci byte output = `<｜channel｜>` (fullwidth, UTF-8 `ef bd 9c`), yang identik dengan varian yang terbukti lolos live.

---

## 3. Kebijakan `.github/` — IKUT UPSTREAM, keep hanya divergence minimal

**Kebijakan aktif:** `.github/` workflow **ikut versi upstream terbaru**, dengan dua divergence yang di-keep: referensi repo `tickernelz/sub2api` di `cla.yml`, dan `continue-on-error: true` pada step `Update DockerHub description` di `release.yml`. Selebihnya (versi tool, action, audit exception, dan step lain) ikut upstream apa adanya.

> Divergence fork lama seperti pin pnpm/golangci khusus fork dan `audit-exceptions.yml` usang sudah dibuang. Registry image di `release.yml` memakai `${{ github.repository_owner }}` secara dinamis, jadi otomatis resolve ke `tickernelz` tanpa hardcode.

| File | Delta fork vs upstream | Alasan |
|---|---|---|
| `cla.yml` | 5 baris: `github.repository == 'tickernelz/sub2api'` (2×) + `path-to-document`/link CLA `github.com/tickernelz/sub2api` (3×) | Guard job CLA hanya jalan di repo fork; link ke CLA.md fork. |
| `release.yml` | 1 baris: `continue-on-error: true` di step `Update DockerHub description` | Step itu bisa `403 Forbidden` (token perms) dan menandai job Release merah walau image sukses publish. Soft-fail supaya publish tetap dianggap sukses. |
| **semua file `.github` lain** | **TIDAK ADA** — 100% ikut upstream | pnpm/golangci/step ikut upstream terbaru. |

**Cara apply `.github` setelah reset ke upstream (prosedur baru):**
```bash
# semua workflow SUDAH benar dari reset ke upstream — TIDAK perlu restore dari fork backup.
# 1. re-apply URL repo tickernelz di cla.yml:
sed -i 's#Wei-Shaw/sub2api#tickernelz/sub2api#g' .github/workflows/cla.yml
# 2. re-apply soft-fail di step DockerHub-description release.yml (tambahkan
#    'continue-on-error: true' tepat di bawah '- name: Update DockerHub description').
# verifikasi: cuma cla.yml + release.yml yang beda dari upstream (5 baris tickernelz + 1 baris continue-on-error)
for f in backend-ci.yml security-scan.yml; do
  diff <(git show wei-shaw/main:.github/workflows/$f) .github/workflows/$f && echo "$f OK (==upstream)"
done
diff <(git show wei-shaw/main:.github/workflows/cla.yml) .github/workflows/cla.yml       # hanya 5 baris tickernelz
diff <(git show wei-shaw/main:.github/workflows/release.yml) .github/workflows/release.yml # hanya 1 baris continue-on-error
```
> ⚠️ **JANGAN** `git checkout <fork-backup> -- .github` lagi (cara lama) — itu membawa balik divergence usang. Cukup reset-ke-upstream + sed cla.yml.

### Divergensi lain di luar `.github/`

| File | Delta fork vs upstream | Alasan |
|---|---|---|
| `.gitignore` | Rule `AGENTS.md` diganti komentar bahwa file sengaja tracked | Supaya dokumen ini tetap tracked & tidak hilang saat sync. Setelah reset ke upstream, hapus/ganti lagi rule `AGENTS.md`, lalu restore file dari safety branch. |
| `backend/go.mod` + 4 assertion CI | ~~`go 1.26.5`~~ **RESOLVED 2026-07-09 (sync #2): upstream sudah adopt go1.26.5** (go.mod + CI guard), jadi ini **BUKAN divergence lagi**. Divergence Go sementara di v0.1.159 sudah gugur otomatis setelah reset. | — (historis: dulu di-bump untuk `GO-2026-5856` crypto/tls ECH leak; upstream nyusul). |

Upstream `.gitignore` meng-ignore `AGENTS.md` dan `CLAUDE.md` (dianggap file lokal). Karena kita justru mau AGENTS.md ini masuk repo, tiap sync **hapus atau ganti rule `AGENTS.md` dengan komentar** dan pastikan file ini ikut ter-commit:
```bash
sed -i '/^AGENTS\.md$/d' .gitignore   # atau edit manual
git add AGENTS.md .gitignore
```

> ⚠️ **VERIFIKASI PAKAI PERINTAH CI YANG SEBENARNYA — bukan `go test ./...` polos.** CI jalanin `make test-unit` = `go test -tags=unit ./...` dan `make test-integration` = `go test -tags=integration ./...`. `go test ./...` polos MELEWATI file ber-`//go:build unit`, jadi error kompilasi (mis. deklarasi ganda dari auto-merge `_test.go`) LOLOS lokal tapi bikin CI merah `FAIL <pkg> [build failed]`. Ini yang bikin CI v0.1.157 merah (cherry-pick OAuth nambah `updateExtraCalls`/`UpdateExtra` yang upstream juga sudah punya). **Selalu tutup gate backend dengan:** `go test -tags=unit ./...`, `go test -tags=integration ./...`, `govulncheck ./...`, dan golangci-lint versi CI. Jalankan `GOTOOLCHAIN=go1.26.5` kalau toolchain lokal beda.

> ℹ️ Step `Update DockerHub description` pernah kena `403 Forbidden` walau image sudah ter-push. Karena itu fork mempertahankan `continue-on-error: true` hanya pada step tersebut. Tetap verifikasi publish dari log GoReleaser (`artifact pushed image=ghcr.io/tickernelz/sub2api:X.Y.Z digest=sha256:...`).

### Divergensi struktural upstream yang mempengaruhi re-apply fitur (per sync 2026-07)

Upstream sering me-refactor/memecah file besar. Yang sudah ketahuan mengubah lokasi hunk fitur di-keep:

| Area | Dulu (fork lama) | Sekarang (upstream) | Dampak re-apply |
|---|---|---|---|
| i18n locales | `frontend/src/i18n/locales/en.ts` + `zh.ts` (monolitik) | modular `locales/<lang>/admin/{accounts,settings}.ts` dst. | Cherry-pick hunk i18n → **modify/delete conflict**. Re-home key: reauth → `admin/accounts.ts` blok `openai`; streamRetry → `admin/settings.ts` (sibling `streamTimeout`, sebelum `rectifier`). |
| admin service | `admin_service.go` monolitik (~4300 baris) | dipecah; `UpdateAccount` → `admin_account.go` | Hunk `applyOpenAIRefreshTokenRecoveredExtra` re-home ke `admin_account.go`. |
| setting handler | `setting_handler.go` | 3 handler streamRetry → `setting_handler_runtime.go` (setelah `UpdateStreamTimeoutSettings`) | — |
| gateway | `gateway_service.go` (7294 baris) | dipecah → `gateway_anthropic_passthrough.go`, `gateway_upstream_response.go`; antigravity → `antigravity_gateway_streaming.go` + `antigravity_gateway_upstream.go` | Wiring watchdog (Fitur B) pindah file — lihat PITFALL WIRING di §2. |

Prinsip umum: kalau cherry-pick fitur kena `modify/delete` atau conflict struktural raksasa (seluruh body file), **ambil versi upstream (`git checkout HEAD -- <file>`) lalu re-apply hunk kecil fitur ke lokasi barunya** — jangan paksa merge body monolitik lama.

---

## 4. Strategi Sync dengan Upstream (WAJIB: full-rewrite, bukan merge)

**Pelajaran mahal:** `git merge wei-shaw/main` menghasilkan puluhan konflik yang salah di-resolve (deklarasi ganda, import ganda, brace tak seimbang, blok fungsi menggantung). **Jangan pakai merge.** Pakai pola full-rewrite + cherry-pick.

### Prosedur baku

```bash
# 0. Pastikan working tree bersih & fetch upstream
git status
git fetch wei-shaw

# 1. Backup state sekarang (WAJIB — recovery net)
git branch backup/pre-rewrite-$(date +%Y%m%d-%H%M%S) HEAD

# 2. Reset main ke upstream (full upstream tree)
git reset --hard wei-shaw/main

# 3. Bersihkan leftover fork-only yang untracked (mis. package yang tak ada di upstream)
git status --porcelain          # cek dulu
# rm -rf <path-fork-only-untracked>

# 4. Cherry-pick commit fitur yang di-keep (urut; cari SHA terbaru berdasarkan subject)
git cherry-pick <sha-backend-soft-handle>
git cherry-pick <sha-frontend-reauth-warning>
git cherry-pick <sha-stream-watchdog>
git cherry-pick <sha-harmony-channel-neutralize>
git cherry-pick <sha-invalid-prompt-observability>
#   -> resolve konflik sesuai §2 dan audit wiring watchdog (Fitur B) + 2 builder/2 response loops (Fitur C)
#   -> pastikan TIDAK ada import 'tickernelz/sub2api' yang bocor:
#      git diff --cached | grep tickernelz   # harus kosong

# 5. Re-apply HANYA divergence minimal (§3); jangan restore seluruh .github
#    - cla.yml: 5 referensi Wei-Shaw/sub2api -> tickernelz/sub2api
#    - release.yml: continue-on-error pada Update DockerHub description
#    - restore AGENTS.md dari safety branch dan unignore di .gitignore

# 6. Bump VERSION (§6)
echo "0.1.xxx" > backend/cmd/server/VERSION

# 7. GATE verifikasi (§5) — WAJIB hijau sebelum push

# 8. Commit sisa (chore) + push
git add .github .gitignore AGENTS.md backend/cmd/server/VERSION
git commit -m "chore: keep fork divergences and bump VERSION to 0.1.xxx"
git push --force-with-lease=refs/heads/main:<verified-old-origin-sha> origin HEAD:main
```

> Kalau nanti commit fitur di-squash/ganti SHA di upstream history, cari ulang dengan:
> `git log --oneline --all --grep="keep oauth accounts schedulable"` dan `--grep="OpenAI OAuth reauth warning"`.

---

## 5. Gate Verifikasi (WAJIB hijau sebelum push)

Jalankan dari root repo. **Jangan push kalau ada yang merah.**

### Backend
```bash
cd backend
unset OPENAI_API_KEY  # CI tidak menyetel ini; kalau local key ada, test comparison akan memanggil OpenAI live
export PATH="$(go env GOPATH)/bin:$PATH"
go build ./...          # harus exit 0
go vet ./...            # harus bersih
go test -tags=unit ./...        # exact CI unit gate
go test -tags=integration ./... # exact CI integration gate
govulncheck ./...               # security gate
# golangci-lint HARUS versi yang sama dgn CI (v2.9), bukan brew default:
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.9.0
"$(go env GOPATH)/bin/golangci-lint" run --timeout=30m   # target: "0 issues."
```
> ⚠️ Mesin ini punya golangci-lint **v2.12.2** via brew di PATH. **Selalu panggil path eksplisit `$(go env GOPATH)/bin/golangci-lint`** untuk pakai v2.9, kalau tidak hasil lokal beda dengan CI.

### Frontend
```bash
cd frontend
export CI=true       # non-interaktif (hindari prompt TTY pnpm)
./node_modules/.bin/eslint . --ext .vue,.js,.jsx,.cjs,.mjs,.ts,.tsx,.cts,.mts
./node_modules/.bin/vue-tsc --noEmit
./node_modules/.bin/vitest run
./node_modules/.bin/vue-tsc -b && ./node_modules/.bin/vite build
```

> Mesin ini memakai pnpm 11, sedangkan CI memakai pnpm 9. Wrapper `pnpm run` dapat memicu dep-status frozen-install dan gagal karena konfigurasi `overrides`; pakai binary existing langsung seperti di atas. Install/update dependency hanya jika memang perlu dan jangan commit artifact lock/workspace dari pnpm 11.

**Pitfall lockfile:** setelah build, pnpm versi baru bisa memindahkan `overrides` dari `pnpm-lock.yaml` ke `pnpm-workspace.yaml` baru. Itu **artifact tool**, bukan perubahan intensional — buang:
```bash
git checkout frontend/pnpm-lock.yaml
rm -f frontend/pnpm-workspace.yaml
```
`backend/internal/web/dist/` adalah artifact build frontend dan **sudah gitignored** — jangan di-commit.

---

## 6. Versi & Release

- Versi produk ada di `backend/cmd/server/VERSION` (mis. `0.1.155`).
- Nilai final **di-drive oleh git tag** via `.github/workflows/release.yml` (step `update-version` menulis ulang file dari tag).
- Rilis dipicu dengan **push tag** `vX.Y.Z`:
  ```bash
  echo "0.1.xxx" > backend/cmd/server/VERSION   # commit ini di main
  git tag -a v0.1.xxx -m "Release v0.1.xxx: ..."
  git push origin v0.1.xxx
  ```
- `release.yml` **hanya build + push image** ke GHCR (`ghcr.io/tickernelz/sub2api`) + Docker Hub (multi-arch amd64/arm64). **Tidak ada deploy/SSH ke server** — aman, tidak me-redeploy production.
- `backend-ci.yml` trigger `on: push` (branch **dan** tag) → tag rilis juga menjalankan test+lint. Pastikan tag menunjuk commit yang gate-nya sudah hijau.

---

## 7. Standar Kerja & Pitfalls

- **Kode produk = ikut upstream.** Kalau ada test/lint upstream yang memang broken di upstream murni (mis. label i18n `透传` vs `passthrough` di `BulkEditAccountModal.spec.ts`), **verifikasi dulu** bahwa `wei-shaw/main` pristine juga gagal (worktree terpisah), baru betulkan test-nya — jangan ubah kode produk untuk menambal test upstream.
- **Jangan pakai subagent** untuk kerjaan repo ini kalau diminta kerjakan sendiri; pemilik pernah minta eksplisit dikerjakan langsung.
- **Force push** ke `origin main` diperbolehkan untuk fork ini (sudah dikonfirmasi), tapi **selalu bikin backup branch dulu** dan simpan `origin/main` sebagai recovery.
- Setelah reset, cek **untracked leftover** fork-only (`git status --porcelain`) — package yang ada di fork tapi tidak di upstream akan tersisa sebagai untracked dan harus dibuang manual.
- Verifikasi klaim "sukses" dengan bukti nyata: image publish dicek dari log GoReleaser (`artifact pushed ... digest=sha256:...`), bukan asumsi.

---

## 8. Ringkasan Struktur Commit yang Diharapkan

Setelah sync bersih, `main` harus terlihat seperti:
```
<chore>   chore: keep fork CI workflows, bump VERSION to 0.1.xxx
<feat>    feat(gateway): record invalid_prompt blocks to ops_error_logs       ← Fitur C Layer 2
<feat>    feat(gateway): neutralize harmony <｜channel｜> token to avoid upstream invalid_prompt block  ← Fitur C
<feat>    feat(gateway): stream stale detection + auto-failover...  ← Fitur B (watchdog)
<feat>    feat(admin): show OpenAI OAuth reauth warning             ← Fitur A (frontend)
<fix>     fix(openai): keep oauth accounts schedulable...           ← Fitur A (backend)
<upstream HEAD = wei-shaw/main>
```
Cuma **5 commit fitur + 1 chore** di atas upstream (3 fitur produk: A = OAuth soft-handle, B = stream watchdog, C = harmony neutralization + observability). Kalau lebih dari itu, ada drift yang perlu ditinjau.
