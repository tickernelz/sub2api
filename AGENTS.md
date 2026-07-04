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

Hanya **satu fitur produk** yang di-keep, terdiri dari 2 commit. Selain ini, ikuti upstream apa adanya.

### Fitur: OpenAI/Codex OAuth jangan auto-disable saat refresh token gagal/reused

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

---

## 3. Divergensi `.github/` yang di-KEEP

`.github/` **selalu pakai versi fork** (jangan pakai versi upstream). Perbedaan penting:

| File | Delta fork vs upstream | Alasan |
|---|---|---|
| `backend-ci.yml` | `golangci-lint version: v2.9` (upstream: v2.12.2) | **Kritis.** Kode upstream cuma lulus lint di v2.9. v2.12.2 menyalakan aturan (gosec G703/G704, staticcheck SA1019, govet inline) yang bikin CI merah di file upstream murni. |
| `backend-ci.yml`, `release.yml`, `security-scan.yml` | `pnpm/action-setup@v4` (upstream: v6) | Kompatibilitas runner. |
| `cla.yml` | repo `tickernelz/sub2api` + link CLA fork | Referensi repo fork, bukan Wei-Shaw. |
| `release.yml` | `continue-on-error: true` di step tertentu (baris ~191, ~209) | Step non-kritis (mis. GitHub Release entry / Docker Hub description) yang bisa kena `Forbidden` tapi tak boleh menggagalkan publish image. |

**Cara restore `.github` setelah reset ke upstream:**
```bash
git checkout <fork-backup-ref> -- .github
```
Lalu verifikasi keempat delta di tabel masih ada. **Jangan lupa cek `golangci-lint version: v2.9`** — ini penyebab CI merah paling umum kalau kelewat.

### Divergensi lain di luar `.github/`

| File | Delta fork vs upstream | Alasan |
|---|---|---|
| `.gitignore` | Baris `AGENTS.md` **dihapus** (upstream meng-ignore-nya di ~baris 127) | Supaya dokumen ini tetap tracked & tidak hilang saat sync. Setelah reset ke upstream, hapus lagi baris `AGENTS.md` dari `.gitignore` lalu `git add AGENTS.md`. |

Upstream `.gitignore` meng-ignore `AGENTS.md` dan `CLAUDE.md` (dianggap file lokal). Karena kita justru mau AGENTS.md ini masuk repo, tiap sync **hapus baris `AGENTS.md` dari `.gitignore`** dan pastikan file ini ikut ter-commit:
```bash
sed -i '/^AGENTS\.md$/d' .gitignore   # atau edit manual
git add AGENTS.md .gitignore
```

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
<feat>    feat(admin): show OpenAI OAuth reauth warning        ← fitur di-keep
<fix>     fix(openai): keep oauth accounts schedulable...      ← fitur di-keep
<upstream HEAD = wei-shaw/main>
```
Cuma **2 commit fitur + 1 chore** di atas upstream. Kalau lebih dari itu, ada drift yang perlu ditinjau.
