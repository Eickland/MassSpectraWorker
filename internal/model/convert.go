package model

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// Алгоритмы центроидирования msconvert
const (
	PeakPickingVendor = "vendor" // алгоритм прибора (Bruker/Thermo…), порог задаёт прибор
	PeakPickingCWT    = "cwt"    // вейвлет ProteoWizard, порог по сигнал/шум
	PeakPickingNone   = "none"   // без центроидирования: профиль как есть
)

var KnownPeakPicking = map[string]bool{
	PeakPickingVendor: true,
	PeakPickingCWT:    true,
	PeakPickingNone:   true,
}

// Типы фильтра threshold msconvert:
// threshold <type> <threshold> <orientation> [<mslevels>]
var KnownThresholdTypes = map[string]bool{
	"absolute":     true, // абсолютная интенсивность
	"bpi-relative": true, // доля от самого сильного пика (0–1)
	"tic-relative": true, // доля от суммарного тока (0–1)
	"count":        true, // число самых интенсивных пиков
}

const maxExtraFilters = 10

var msLevelsRe = regexp.MustCompile(`^[1-9][0-9]*(-([1-9][0-9]*)?)?$`)

// ConvertParams - параметры конвертации исходного файла прибора в .mzML (msconvert).
// Хранятся в параметрах задачи и прогона, поэтому видны в истории прогонов.
type ConvertParams struct {
	PeakPicking string `json:"peak_picking"`        // vendor | cwt | none
	MsLevels    string `json:"ms_levels,omitempty"` // уровни MS для центроидирования и порога: "1-", "1", "1-2"

	// Только для cwt
	CwtSNR       float64 `json:"cwt_snr,omitempty"`        // мин. сигнал/шум
	CwtPeakSpace float64 `json:"cwt_peak_space,omitempty"` // мин. расстояние между пиками, m/z

	// Фильтр интенсивности после центроидирования; пусто — без фильтра
	ThresholdType string  `json:"threshold_type,omitempty"`
	Threshold     float64 `json:"threshold,omitempty"`

	// Окно m/z (mzWindow); 0 и 0 — весь диапазон
	MzMin float64 `json:"mz_min,omitempty"`
	MzMax float64 `json:"mz_max,omitempty"`

	// Для профиля (none): по умолчанию нулевые точки удаляются (zeroSamples removeExtra)
	KeepZeroSamples bool `json:"keep_zero_samples,omitempty"`

	// Дополнительные фильтры msconvert, по одному на элемент, без "--filter"
	ExtraFilters []string `json:"extra_filters,omitempty"`
}

var (
	defaultConvertMu sync.RWMutex
	defaultConvert   = ConvertParams{
		PeakPicking:  PeakPickingVendor,
		MsLevels:     "1-",
		CwtSNR:       1,
		CwtPeakSpace: 0.001, // по умолчанию в msconvert 0.1 — для FTICR склеит изобары
	}
)

// DefaultConvert - параметры конвертации по умолчанию (с учётом переменных окружения)
func DefaultConvert() ConvertParams {
	defaultConvertMu.RLock()
	defer defaultConvertMu.RUnlock()
	return defaultConvert
}

// SetDefaultConvert - задаёт умолчания (вызывается при старте из конфигурации).
// Незаданные поля c остаются встроенными умолчаниями.
func SetDefaultConvert(c ConvertParams) error {
	defaultConvertMu.Lock()
	base := defaultConvert
	defaultConvertMu.Unlock()

	if c.PeakPicking != "" {
		base.PeakPicking = strings.ToLower(strings.TrimSpace(c.PeakPicking))
	}
	if c.MsLevels != "" {
		base.MsLevels = strings.TrimSpace(c.MsLevels)
	}
	if c.CwtSNR > 0 {
		base.CwtSNR = c.CwtSNR
	}
	if c.CwtPeakSpace > 0 {
		base.CwtPeakSpace = c.CwtPeakSpace
	}
	if err := base.Validate(); err != nil {
		return err
	}

	defaultConvertMu.Lock()
	defaultConvert = base
	defaultConvertMu.Unlock()
	return nil
}

// ApplyDefaults - заполняет незаданные поля и нормализует значения
func (c *ConvertParams) ApplyDefaults() {
	d := DefaultConvert()
	c.PeakPicking = strings.ToLower(strings.TrimSpace(c.PeakPicking))
	if c.PeakPicking == "" {
		c.PeakPicking = d.PeakPicking
	}
	c.MsLevels = strings.TrimSpace(c.MsLevels)
	if c.MsLevels == "" {
		c.MsLevels = d.MsLevels
	}
	if c.CwtSNR == 0 {
		c.CwtSNR = d.CwtSNR
	}
	if c.CwtPeakSpace == 0 {
		c.CwtPeakSpace = d.CwtPeakSpace
	}
	c.ThresholdType = strings.ToLower(strings.TrimSpace(c.ThresholdType))
	if c.ThresholdType == "none" {
		c.ThresholdType = ""
	}
	if c.ThresholdType == "" {
		c.Threshold = 0
	}
	var extra []string
	for _, f := range c.ExtraFilters {
		if f = strings.TrimSpace(f); f != "" {
			extra = append(extra, f)
		}
	}
	c.ExtraFilters = extra
}

func (c ConvertParams) Validate() error {
	if !KnownPeakPicking[c.PeakPicking] {
		return fmt.Errorf("convert: unknown peak_picking %q (vendor|cwt|none)", c.PeakPicking)
	}
	if !msLevelsRe.MatchString(c.MsLevels) {
		return fmt.Errorf("convert: ms_levels %q must look like 1, 1- or 1-2", c.MsLevels)
	}
	if c.PeakPicking == PeakPickingCWT {
		if !(c.CwtSNR > 0 && c.CwtSNR <= 1000) {
			return fmt.Errorf("convert: cwt_snr must be in (0, 1000]")
		}
		if !(c.CwtPeakSpace > 0 && c.CwtPeakSpace <= 10) {
			return fmt.Errorf("convert: cwt_peak_space must be in (0, 10] m/z")
		}
	}
	if c.ThresholdType != "" {
		if !KnownThresholdTypes[c.ThresholdType] {
			return fmt.Errorf("convert: unknown threshold_type %q", c.ThresholdType)
		}
		if !(c.Threshold > 0) || math.IsInf(c.Threshold, 0) {
			return fmt.Errorf("convert: threshold must be positive")
		}
		switch c.ThresholdType {
		case "bpi-relative", "tic-relative":
			if c.Threshold > 1 {
				return fmt.Errorf("convert: %s threshold is a fraction 0–1 (0.002 = 0.2%%)", c.ThresholdType)
			}
		case "count":
			if c.Threshold != math.Trunc(c.Threshold) {
				return fmt.Errorf("convert: count threshold must be an integer")
			}
		}
	}
	if c.MzMin < 0 || c.MzMax < 0 {
		return fmt.Errorf("convert: m/z window must be non-negative")
	}
	if (c.MzMin > 0) != (c.MzMax > 0) {
		return fmt.Errorf("convert: set both mz_min and mz_max, or neither")
	}
	if c.MzMax > 0 && c.MzMin >= c.MzMax {
		return fmt.Errorf("convert: mz_min must be less than mz_max")
	}
	if len(c.ExtraFilters) > maxExtraFilters {
		return fmt.Errorf("convert: at most %d extra filters", maxExtraFilters)
	}
	for _, f := range c.ExtraFilters {
		if len(f) > 300 {
			return fmt.Errorf("convert: extra filter is too long")
		}
		if strings.HasPrefix(f, "-") {
			return fmt.Errorf("convert: extra filter %q must not start with '-' (write the filter without --filter)", f)
		}
		if strings.ContainsFunc(f, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
			return fmt.Errorf("convert: extra filter contains control characters")
		}
		fields := strings.Fields(f)
		if len(fields) == 0 {
			return fmt.Errorf("convert: empty extra filter")
		}
		if strings.EqualFold(fields[0], "peakPicking") {
			return fmt.Errorf("convert: choose peak picking with peak_picking, not in extra filters")
		}
	}
	return nil
}

func fmtNum(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }

// Filters - фильтры msconvert в порядке применения. Центроидирование
// должно быть первым: vendor-алгоритм работает только с исходными данными прибора.
func (c ConvertParams) Filters() []string {
	var out []string
	switch c.PeakPicking {
	case PeakPickingVendor:
		out = append(out, "peakPicking vendor msLevel="+c.MsLevels)
	case PeakPickingCWT:
		out = append(out, fmt.Sprintf("peakPicking cwt snr=%s peakSpace=%s msLevel=%s",
			fmtNum(c.CwtSNR), fmtNum(c.CwtPeakSpace), c.MsLevels))
	}
	if c.MzMax > 0 {
		out = append(out, fmt.Sprintf("mzWindow [%s,%s]", fmtNum(c.MzMin), fmtNum(c.MzMax)))
	}
	if c.ThresholdType != "" {
		out = append(out, fmt.Sprintf("threshold %s %s most-intense %s",
			c.ThresholdType, fmtNum(c.Threshold), c.MsLevels))
	}
	if c.PeakPicking == PeakPickingNone && !c.KeepZeroSamples {
		out = append(out, "zeroSamples removeExtra")
	}
	return append(out, c.ExtraFilters...)
}

// Args - аргументы msconvert после входного файла и каталога вывода
func (c ConvertParams) Args() []string {
	args := []string{"--mzML"}
	for _, f := range c.Filters() {
		args = append(args, "--filter", f)
	}
	return append(args, "--verbose")
}

// Tag - короткий хэш набора фильтров. Входит в имя кэшированного .mzML,
// чтобы файл, сконвертированный с другими параметрами, не переиспользовался.
func (c ConvertParams) Tag() string {
	sum := sha1.Sum([]byte(strings.Join(c.Filters(), "\n")))
	return hex.EncodeToString(sum[:4])
}

// Centroided - ожидаются ли в результате центроидные спектры
func (c ConvertParams) Centroided() bool { return c.PeakPicking != PeakPickingNone }

// CommandLine - команда для показа пользователю (аргументы с пробелами в кавычках)
func CommandLine(exe, src, outDir string, c ConvertParams) string {
	parts := []string{exe, src, "-o", outDir}
	parts = append(parts, c.Args()...)
	for i, p := range parts {
		if p == "" || strings.ContainsAny(p, " \t\"'[]") {
			parts[i] = strconv.Quote(p)
		}
	}
	return strings.Join(parts, " ")
}
