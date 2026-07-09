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

> ⚠️ **SHA berubah tiap sync.** Angka commit di bawah (`74fd9323`, `6676fa36`, `afb57148`) adalah SHA dari sync 2026-07 di atas upstream lama. Setiap kali reset ke upstream + cherry-pick, SHA fitur akan berubah. Cari ulang dengan `git log --oneline --all --grep=...` (lihat catatan di §4).

### Fitur A: OpenAI/Codex OAuth jangan auto-disable saat refresh token gagal/reused

Akun OpenAI OAuth **tidak boleh** langsung di-`SetError`/unschedule ketika refresh token gagal karena `refresh_token_reused`. Alasannya: `refresh_token_reused` cuma menandakan refresh token sudah dikonsumsi/dirotasi — **bukan** bukti access token saat ini tidak valid. Akun tetap dibiarkan schedulable, dan UI menampilkan warning "reauth required".

Dua commit sumber (fork-only, tidak ada di upstream):

| Commit | Judul | Cakupan |
|---|---|---|
| `c53d7c69` | `fix(openai): keep oauth accounts schedulable on reused refresh token` | Backend soft-handle |
| `e86ae7c9` | `feat(admin): show OpenAI OAuth reauth warning` | Frontend warning UI |

**File yang disentuh (referensi saat re-apply):**

Backend (`c53d7c69`):
- `backend/internal/service/openai_refresh_token_state.go` *(file baru — inti logika)*
- `backend/internal/service/token_refresh_service.go` *(titik keputusan `isNonRetryableRefreshError`)*
- `backend/internal/service/openai_token_provider.go`
- `backend/internal/service/token_refresher.go`
- `backend/internal/handler/admin/account_handler.go`
- `backend/internal/repository/account_repo.go`
- `backend/internal/service/admin_service.go`
- `backend/internal/service/token_refresh_service_test.go`

Frontend (`e86ae7c9`):
- `frontend/src/components/admin/account/AccountActionMenu.vue` *(computed `hasOpenAIRefreshTokenReauthRequired`)*
- `frontend/src/views/admin/AccountsView.vue`
- `frontend/src/types/index.ts`
- `frontend/src/i18n/locales/en.ts`, `frontend/src/i18n/locales/zh.ts`
- `frontend/src/views/admin/__tests__/AccountsView.openaiReauthWarning.spec.ts`
- `Makefile`

**Titik konflik yang sudah diketahui saat re-apply:**
1. `token_refresh_service.go` cabang `if isNonRetryableRefreshError(err) {` — upstream pakai `logredact.RedactText(err.Error())`, commit fork pakai `fmt.Sprintf`. **Gabungkan:** pertahankan blok soft-handle (`shouldSoftHandleOpenAIRefreshTokenReused` → `markOpenAIRefreshTokenReused` → `ensureOpenAIPrivacy` → `return err`) **DAN** pakai `logredact.RedactText` dari upstream untuk `errorMsg`.
2. `AccountActionMenu.vue` — upstream punya `isShadow`/`isOpenAIOAuthParent`/`supportsPrivacy` versi shadow-aware. **Keep versi upstream** + tambahkan computed `hasOpenAIRefreshTokenReauthRequired`; jangan pakai `supportsPrivacy` versi lama dari commit fork.

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

Satu commit sumber (fork-only): `afb57148` `feat(gateway): stream stale detection + auto-failover across all providers` (~1900 baris, 19 file). Default enabled (TTFT 60s, gap-warn 10s, gap-timeout 30s).

**File inti (port verbatim — file baru, 0 collision dengan upstream):**
- `backend/internal/service/stream_stale_watchdog.go` *(watchdog + injectable clock)*
- `backend/internal/service/stream_retry_settings.go` *(settings + resolver berlapis + Get/Set)*
- `backend/internal/service/stream_watchdog_integration.go` *(glue: cache, `newStreamWatchdogForPlatform`, `decideStreamStall`, metrics)*
- `+ stream_stale_watchdog_test.go`, `stream_retry_settings_test.go`

**Admin surface (additive):** `setting_handler_runtime.go` (3 handler: Get/Update StreamRetry + Metrics), `dto/settings.go`, `server/routes/admin.go` (3 route), `domain_constants.go` (`SettingKeyStreamRetrySettings`), frontend `api/admin/settings.ts` + `SettingsView.vue` + i18n `streamRetry` block.

> ⚠️ **PITFALL WIRING (pelajaran sync 2026-07):** upstream me-refactor besar gateway — `gateway_service.go` (7294→1289 baris) dipecah jadi `gateway_anthropic_passthrough.go`, `gateway_upstream_response.go`; antigravity dipecah jadi `antigravity_gateway_streaming.go` + `antigravity_gateway_upstream.go`. **10 titik wiring watchdog pindah file.** Cherry-pick `afb57148` mentah GAGAL di titik ini (hunk tak ketemu context). **JANGAN cherry-pick buta** — re-implement wiring ke lokasi loop upstream yang baru: (1) init watchdog setelah `intervalCh = intervalTicker.C`, (2) `staleWatchdog.OnUpstreamEvent()` setelah `line := ev.line`, (3) 3 select-arm (`staleTTFTCh`/`staleGapWarnCh`/`staleGapTimeoutCh`) sebelum `case <-keepaliveCh`. Loop body upstream masih byte-identical (pure move-split), jadi anchor tetap sama; verifikasi dengan strip-watchdog-diff bahwa loop stripped == upstream sebelum swap.

---

## 3. Kebijakan `.github/` — IKUT UPSTREAM, keep hanya URL repo `tickernelz`

**Kebijakan baru (2026-07-09, atas permintaan pemilik):** `.github/` workflow **ikut versi upstream terbaru**. SATU-SATUNYA divergence yang di-keep adalah **referensi repo `tickernelz/sub2api` di `cla.yml`** (guard `github.repository == 'tickernelz/sub2api'` + link `path-to-document` CLA). Selebihnya (versi tool, action, step) ikut upstream apa adanya.

> Alasan pergantian kebijakan: divergence fork lama (pnpm `@v4`, `golangci-lint v2.9` pin, `continue-on-error`, `audit-exceptions.yml` expires_on lama) sudah **usang** — upstream sendiri sudah pindah ke `golangci-lint v2.9` (jadi bukan divergence lagi) dan divergence sisanya cuma bikin friksi tanpa manfaat nyata. Registry image di `release.yml` pakai `${{ github.repository_owner }}` **dinamis**, jadi otomatis resolve ke `tickernelz` tanpa perlu hardcode.

| File | Delta fork vs upstream | Alasan |
|---|---|---|
| `cla.yml` | 5 baris: `github.repository == 'tickernelz/sub2api'` (2×) + `path-to-document`/link CLA `github.com/tickernelz/sub2api` (3×) | Guard job CLA hanya jalan di repo fork; link ke CLA.md fork. |
| **semua file `.github` lain** | **TIDAK ADA** — 100% ikut upstream | pnpm/golangci/step ikut upstream terbaru. |

**Cara apply `.github` setelah reset ke upstream (prosedur baru):**
```bash
# semua workflow SUDAH benar dari reset ke upstream — TIDAK perlu restore dari fork backup.
# Cukup re-apply URL repo tickernelz di cla.yml:
sed -i 's#Wei-Shaw/sub2api#tickernelz/sub2api#g' .github/workflows/cla.yml
# verifikasi: cuma cla.yml yang beda dari upstream, dan bedanya cuma 5 baris tickernelz
for f in backend-ci.yml release.yml security-scan.yml; do
  diff <(git show wei-shaw/main:.github/workflows/$f) .github/workflows/$f && echo "$f OK (==upstream)"
done
diff <(git show wei-shaw/main:.github/workflows/cla.yml) .github/workflows/cla.yml   # hanya 5 baris tickernelz
```
> ⚠️ **JANGAN** `git checkout <fork-backup> -- .github` lagi (cara lama) — itu membawa balik divergence usang. Cukup reset-ke-upstream + sed cla.yml.

### Divergensi lain di luar `.github/`

| File | Delta fork vs upstream | Alasan |
|---|---|---|
| `.gitignore` | Baris `AGENTS.md` **dihapus** (upstream meng-ignore-nya di ~baris 127) | Supaya dokumen ini tetap tracked & tidak hilang saat sync. Setelah reset ke upstream, hapus lagi baris `AGENTS.md` dari `.gitignore` lalu `git add AGENTS.md`. |

Upstream `.gitignore` meng-ignore `AGENTS.md` dan `CLAUDE.md` (dianggap file lokal). Karena kita justru mau AGENTS.md ini masuk repo, tiap sync **hapus baris `AGENTS.md` dari `.gitignore`** dan pastikan file ini ikut ter-commit:
```bash
sed -i '/^AGENTS\.md$/d' .gitignore   # atau edit manual
git add AGENTS.md .gitignore
```

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

# 4. Cherry-pick 2 commit fitur yang di-keep (urut)
git cherry-pick c53d7c69ffd2bf3010f3854065869c400ec94993   # backend soft-handle
git cherry-pick e86ae7c968f5c657008b2fbb1465c2e323b19451   # frontend reauth warning
#   -> resolve konflik sesuai §2 (2 titik yang sudah diketahui)
#   -> pastikan TIDAK ada import 'tickernelz/sub2api' yang bocor:
#      git diff --cached | grep tickernelz   # harus kosong

# 5. Restore .github fork + cek delta (§3)
git checkout backup/<fork-backup-terbaru> -- .github
grep -n 'version: v2' .github/workflows/backend-ci.yml   # harus v2.9

# 6. Bump VERSION (§6)
echo "0.1.xxx" > backend/cmd/server/VERSION

# 7. GATE verifikasi (§5) — WAJIB hijau sebelum push

# 8. Commit sisa (chore) + push
git add .github backend/cmd/server/VERSION
git commit -m "chore: keep fork CI workflows, bump VERSION to 0.1.xxx"
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
go test ./...           # semua paket ok, 0 FAIL
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
