# Education Map: paket instalasi awal

Paket ini memasang ArcadeDB dan backend Go awal, lalu membawa Anda sampai proses onboarding selesai dan graf awal seorang anak tersimpan.

## Isi paket

| Bagian | Isi |
|---|---|
| `docker-compose.yml` | Dua layanan: `arcadedb` (26.10.2) dan `app` (backend Go) |
| `.env.example` | Contoh konfigurasi. Salin menjadi `.env` |
| `backend/` | Backend Go tanpa dependensi luar: klien HTTP ArcadeDB, migrasi skema, katalog awal, akun, API onboarding |
| `backend/migrations/001_schema.sql` | 19 tipe vertex, 34 tipe edge, indeks unik pada `id` |
| `backend/web/onboarding.html` | Wizard onboarding |
| `backend/web/index.html` | Halaman utama sederhana setelah onboarding |

Belum termasuk: tampilan graf, input berkala, unggah dokumen, object storage, worker saran, dan sambungan ke AI platform. Semuanya dibangun di atas paket ini mengikuti halaman arsitektur.

## Status pengujian

- Backend lulus `go vet` dan `go test`. Pengujian memakai tiruan HTTP API ArcadeDB, dari instalasi kosong sampai onboarding selesai.
- Wizard sudah dijalankan di browser terhadap backend dengan tiruan yang sama.
- **Belum diuji terhadap server ArcadeDB sungguhan dan belum dijalankan lewat Docker.** Titik yang paling mungkin perlu penyesuaian: sintaks Cypher pada `backend/onboarding.go` dan `backend/seed.go`, serta bentuk hasil `count()`.

## Prasyarat

- Docker Engine 24 atau lebih baru, dengan plugin Compose (`docker compose version`).
- RAM kosong sekitar 2 GB dan port 8080 yang belum dipakai.
- Akses internet saat pertama kali, untuk menarik image.

## Langkah instalasi

1. Buka paket dan masuk ke foldernya.

   ```sh
   unzip education-map.zip
   cd education-map
   ```

2. Buat berkas konfigurasi dan isi dua rahasianya.

   ```sh
   cp .env.example .env
   openssl rand -hex 16   # tempel sebagai ARCADEDB_ROOT_PASSWORD
   openssl rand -hex 32   # tempel sebagai SESSION_SECRET
   ```

3. Bangun dan jalankan.

   ```sh
   docker compose up -d --build
   ```

4. Tunggu backend siap. Saat pertama hidup, backend menunggu ArcadeDB, membuat database `education`, menjalankan migrasi, lalu mengisi katalog awal.

   ```sh
   docker compose logs -f app
   ```

   Baris yang ditunggu, berurutan:

   ```
   waiting for ArcadeDB at http://arcadedb:2480
   migration applied: 001_schema
   catalog seeded: seed-001-catalog
   listening on :8080 (database "education")
   ```

5. Buka `http://localhost:8080`. Karena belum ada akun, halaman dialihkan ke `/onboarding`.

## Langkah onboarding

| Langkah | Isi | Wajib |
|---|---|---|
| Akun pemilik | Email dan kata sandi (minimal 10 karakter) | Ya, sekali per instalasi |
| Tentang anak | Nama panggilan dan tanggal lahir | Ya |
| Posisi sekarang | Jenjang dan kelas, sudah diisi dari umur | Ya |
| Sekolah | Nama, jenis, kurikulum | Tidak |
| Gambaran kemampuan | Level dan kesenangan per bidang | Tidak |
| Minat, olahraga, metode | Pilihan dari katalog atau entri baru | Tidak |
| Lomba | Nama, jenis, tahun, babak tertinggi, hasil | Tidak |
| Tujuan dan batasan | Batas jam per minggu, jurusan, izin saran AI | Tidak |
| Tinjau | Ringkasan, lalu **Selesai dan buat graf** | Ya |

Langkah yang tidak relevan untuk umur anak disembunyikan. Draf tersimpan di server setiap ada perubahan, jadi wizard bisa ditutup dan dilanjutkan. Setelah **Selesai**, graf ditulis dalam satu transaksi dan halaman utama terbuka.

## Memeriksa hasil

Buka ArcadeDB Studio di `http://localhost:2480`, masuk sebagai `root` dengan kata sandi dari `.env`, pilih database `education`, lalu jalankan:

```cypher
MATCH (c:Child)-[r]->(x) RETURN c.nickname, type(r), x.id
```

## Perintah sehari-hari

```sh
docker compose ps                 # status
docker compose logs -f app        # log backend
docker compose restart app        # hidupkan ulang backend
docker compose down               # berhenti, data tetap ada
docker compose down -v            # berhenti dan HAPUS semua data
docker compose up -d --build      # setelah mengubah kode backend
```

Menambah anak kedua: buka `http://localhost:8080/onboarding?add`.

## Bila ada masalah

| Gejala | Yang diperiksa |
|---|---|
| `set ARCADEDB_ROOT_PASSWORD in .env` | Berkas `.env` belum dibuat atau nilainya kosong |
| Log berhenti di `waiting for ArcadeDB` | `docker compose logs arcadedb`. Kata sandi root harus minimal 8 karakter |
| `migration 001_schema: ... failed` | Sintaks SQL tidak cocok dengan versi ArcadeDB. Sesuaikan `backend/migrations/001_schema.sql` |
| Onboarding gagal saat **Selesai** | `docker compose logs app` menampilkan pernyataan Cypher yang ditolak |
| Port 8080 sudah dipakai | Ubah `APP_PORT` di `.env` |
| Keluar sendiri setelah restart | `SESSION_SECRET` kosong atau kurang dari 32 karakter |

## Sebelum dipakai di luar jaringan rumah

- Pasang reverse proxy dengan HTTPS, lalu set `COOKIE_SECURE=true`.
- Biarkan port 2480 hanya terbuka untuk mesin ini, seperti bawaan paket.
- Jadwalkan cadangan volume `arcadedb-data`, dan uji pemulihannya.

## Menjalankan pengujian backend

```sh
cd backend
go vet ./... && go test ./...
```
