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

Ada **dua fitur produk** yang di-keep, total 3 commit. Selain ini, ikuti upstream apa adanya.

> ⚠️ **SHA berubah tiap sync.** SHA aktif setelah sync 2026-07-11 di atas upstream `e316ebf52` adalah `6946ce8bc`, `40f41af36`, dan `4c5c53823`. Setiap reset + cherry-pick menghasilkan SHA baru; cari ulang berdasarkan judul commit dengan `git log --oneline --all --grep=...` (lihat §4).

### Fitur A: OpenAI/Codex OAuth jangan auto-disable saat refresh token gagal/reused

Akun OpenAI OAuth **tidak boleh** langsung di-`SetError`/unschedule ketika refresh token gagal karena `refresh_token_reused`. Alasannya: `refresh_token_reused` cuma menandakan refresh token sudah dikonsumsi/dirotasi — **bukan** bukti access token saat ini tidak valid. Akun tetap dibiarkan schedulable, dan UI menampilkan warning "reauth required".

Dua commit sumber (fork-only, tidak ada di upstream):

| Commit | Judul | Cakupan |
|---|---|---|
| `6946ce8bc` | `fix(openai): keep oauth accounts schedulable on reused refresh token` | Backend soft-handle |
| `40f41af36` | `feat(admin): show OpenAI OAuth reauth warning` | Frontend warning UI |

**File yang disentuh (referensi saat re-apply):**

Backend (`6946ce8bc`):
- `backend/internal/service/openai_refresh_token_state.go` *(file baru — inti logika)*
- `backend/internal/service/token_refresh_service.go` *(titik keputusan `isNonRetryableRefreshError`)*
- `backend/internal/service/openai_token_provider.go`
- `backend/internal/service/token_refresher.go`
- `backend/internal/handler/admin/account_handler.go`
- `backend/internal/repository/account_repo.go`
- `backend/internal/service/admin_account.go`
- `backend/internal/service/token_refresh_service_test.go`

Frontend (`40f41af36`):
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

Satu commit sumber (fork-only): `4c5c53823` `feat(gateway): stream stale detection + auto-failover across all providers` (~1900 baris, 20 file). Default enabled (TTFT 60s, gap-warn 10s, gap-timeout 30s).

**File inti (port verbatim — file baru, 0 collision dengan upstream):**
- `backend/internal/service/stream_stale_watchdog.go` *(watchdog + injectable clock)*
- `backend/internal/service/stream_retry_settings.go` *(settings + resolver berlapis + Get/Set)*
- `backend/internal/service/stream_watchdog_integration.go` *(glue: cache, `newStreamWatchdogForPlatform`, `decideStreamStall`, metrics)*
- `+ stream_stale_watchdog_test.go`, `stream_retry_settings_test.go`

**Admin surface (additive):** `setting_handler_runtime.go` (3 handler: Get/Update StreamRetry + Metrics), `dto/settings.go`, `server/routes/admin.go` (3 route), `domain_constants.go` (`SettingKeyStreamRetrySettings`), frontend `api/admin/settings.ts` + `SettingsView.vue` + i18n `streamRetry` block.

> ⚠️ **PITFALL WIRING (pelajaran sync 2026-07):** upstream me-refactor besar gateway — `gateway_service.go` (7294→1289 baris) dipecah jadi `gateway_anthropic_passthrough.go`, `gateway_upstream_response.go`; antigravity dipecah jadi `antigravity_gateway_streaming.go` + `antigravity_gateway_upstream.go`. **10 titik wiring watchdog pindah file.** Jika cherry-pick mentah gagal atau upstream mengubah loop, **JANGAN merge body lama secara buta** — re-implement wiring ke lokasi loop upstream yang baru: (1) init watchdog setelah `intervalCh = intervalTicker.C`, (2) `staleWatchdog.OnUpstreamEvent()` setelah `line := ev.line`, (3) 3 select-arm (`staleTTFTCh`/`staleGapWarnCh`/`staleGapTimeoutCh`) sebelum `case <-keepaliveCh`. Setelah re-apply, audit total 10 init, 10 event-reset, dan 30 `decideStreamStall` call.

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

# 4. Cherry-pick 3 commit fitur yang di-keep (urut; cari SHA terbaru berdasarkan subject)
git cherry-pick <sha-backend-soft-handle>
git cherry-pick <sha-frontend-reauth-warning>
git cherry-pick <sha-stream-watchdog>
#   -> resolve konflik sesuai §2 dan audit wiring watchdog
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
git push --force origin main       # force push OK untuk fork ini (sudah dikonfirmasi pemilik)
```

> Kalau nanti commit fitur di-squash/ganti SHA di upstream history, cari ulang dengan:
> `git log --oneline --all --grep="keep oauth accounts schedulable"` dan `--grep="OpenAI OAuth reauth warning"`.

---

## 5. Gate Verifikasi (WAJIB hijau sebelum push)

Jalankan dari root repo. **Jangan push kalau ada yang merah.**

### Backend
```bash
cd backend
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
pnpm install --no-frozen-lockfile   # rekonsiliasi node_modules ke deps upstream
pnpm run typecheck   # 0 error
pnpm run lint:check  # exit 0
pnpm run test:run    # semua vitest lulus
pnpm run build       # vue-tsc + vite build sukses (warning chunk-size = kosmetik, abaikan)
```

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
<feat>    feat(gateway): stream stale detection + auto-failover...  ← Fitur B (watchdog)
<feat>    feat(admin): show OpenAI OAuth reauth warning             ← Fitur A (frontend)
<fix>     fix(openai): keep oauth accounts schedulable...           ← Fitur A (backend)
<upstream HEAD = wei-shaw/main>
```
Cuma **3 commit fitur + 1 chore** di atas upstream (2 fitur produk: A = OAuth soft-handle, B = stream watchdog). Kalau lebih dari itu, ada drift yang perlu ditinjau.
