// Package backup mencadangkan database Onobill secara periodik.
// SQLite: file-copy (VACUUM INTO). MySQL: mysqldump bila tersedia.
// Hasil disimpan di direktori backups/ dengan retensi N file terbaru.
package backup

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"
)

// Config sumber DB.
type Config struct {
	Driver string // "sqlite" | "mysql"
	Path   string // sqlite file path
	Host   string // mysql
	Port   string
	Name   string
	User   string
	Pass   string
}

// Service melakukan backup & rotasi.
type Service struct {
	db    *gorm.DB
	cfg   Config
	dir   string
	keep  int
}

// NewService membuat service backup. dir default "backups".
func NewService(db *gorm.DB, cfg Config, dir string, keep int) *Service {
	if dir == "" {
		dir = "backups"
	}
	if keep <= 0 {
		keep = 7
	}
	return &Service{db: db, cfg: cfg, dir: dir, keep: keep}
}

// Run menjalankan satu siklus backup. Aman dipanggil berulang.
func (s *Service) Run() error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return fmt.Errorf("mkdir backup: %w", err)
	}
	stamp := time.Now().Format("20060102-150405")

	var out string
	var err error
	switch s.cfg.Driver {
	case "mysql":
		out = filepath.Join(s.dir, "onobill-"+stamp+".sql")
		err = s.dumpMySQL(out)
	default: // sqlite
		out = filepath.Join(s.dir, "onobill-"+stamp+".db")
		err = s.backupSQLite(out)
	}
	if err != nil {
		return err
	}
	log.Printf("[backup] tersimpan: %s", out)
	s.rotate()
	return nil
}

// backupSQLite memakai VACUUM INTO agar konsisten (aman saat DB dipakai).
func (s *Service) backupSQLite(out string) error {
	// Hindari injection: nama file sudah digenerate internal.
	stmt := fmt.Sprintf("VACUUM INTO '%s'", strings.ReplaceAll(out, "'", "''"))
	if err := s.db.Exec(stmt).Error; err != nil {
		// Fallback: copy file mentah bila VACUUM INTO tidak didukung.
		return s.copyFile(s.cfg.Path, out)
	}
	return nil
}

func (s *Service) copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("baca sqlite: %w", err)
	}
	return os.WriteFile(dst, data, 0o644)
}

// dumpMySQL memanggil mysqldump; error jelas bila binary tidak ada.
func (s *Service) dumpMySQL(out string) error {
	bin, err := exec.LookPath("mysqldump")
	if err != nil {
		return fmt.Errorf("mysqldump tidak ditemukan di PATH")
	}
	args := []string{
		"-h", s.cfg.Host,
		"-P", s.cfg.Port,
		"-u", s.cfg.User,
	}
	if s.cfg.Pass != "" {
		args = append(args, "-p"+s.cfg.Pass)
	}
	args = append(args, s.cfg.Name)
	cmd := exec.Command(bin, args...)
	f, err := os.Create(out)
	if err != nil {
		return err
	}
	defer f.Close()
	cmd.Stdout = f
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("mysqldump: %w", err)
	}
	return nil
}

// rotate menghapus backup lama melebihi keep.
func (s *Service) rotate() {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasPrefix(e.Name(), "onobill-") {
			files = append(files, e.Name())
		}
	}
	if len(files) <= s.keep {
		return
	}
	sort.Strings(files) // nama berawalan timestamp -> urut kronologis
	for _, old := range files[:len(files)-s.keep] {
		_ = os.Remove(filepath.Join(s.dir, old))
	}
}
