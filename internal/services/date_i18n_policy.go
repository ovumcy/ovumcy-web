package services

import (
	"fmt"
	"strings"
	"time"
)

// localizedDateNames carries every calendar form one language needs. One entry
// per language replaces the five parallel maps this file used to keep: adding
// a language is a single literal in a single place instead of five coordinated
// edits that nothing forces you to finish, and a missing form shows up as an
// empty string in one table rather than as an English date on one surface.
// TestLocalizedDateFormsCoverEveryRequiredLocale is what fails when an entry is
// incomplete or absent.
//
// The three month forms are genuinely different words, not a duplication:
//   - months is the standalone heading form, title-cased where the UI wants a
//     capital (es "Enero", fr "Janvier", it "Gennaio") even though the language
//     writes months lowercase in running text;
//   - monthsLong is that running-text form, which for Russian is also the
//     required genitive ("января", not "Январь");
//   - monthsShort is the abbreviation.
//
// The arrays are fixed-size on purpose: time.Month() is always 1-12 and
// time.Weekday() always 0-6, so every index below is in range by construction
// and no length check stands between the lookup and the rendered string.
type localizedDateNames struct {
	months        [12]string
	monthsLong    [12]string
	monthsShort   [12]string
	weekdaysShort [7]string
	weekdaysLong  [7]string
}

var dateNames = map[string]localizedDateNames{
	"de": {
		months:        [12]string{"Januar", "Februar", "März", "April", "Mai", "Juni", "Juli", "August", "September", "Oktober", "November", "Dezember"},
		monthsLong:    [12]string{"Januar", "Februar", "März", "April", "Mai", "Juni", "Juli", "August", "September", "Oktober", "November", "Dezember"},
		monthsShort:   [12]string{"Jan.", "Feb.", "Mär.", "Apr.", "Mai", "Juni", "Juli", "Aug.", "Sep.", "Okt.", "Nov.", "Dez."},
		weekdaysShort: [7]string{"So.", "Mo.", "Di.", "Mi.", "Do.", "Fr.", "Sa."},
		weekdaysLong:  [7]string{"Sonntag", "Montag", "Dienstag", "Mittwoch", "Donnerstag", "Freitag", "Samstag"},
	},
	"en": {
		months:        [12]string{"January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"},
		monthsLong:    [12]string{"January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"},
		monthsShort:   [12]string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"},
		weekdaysShort: [7]string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"},
		weekdaysLong:  [7]string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"},
	},
	"es": {
		months:        [12]string{"Enero", "Febrero", "Marzo", "Abril", "Mayo", "Junio", "Julio", "Agosto", "Septiembre", "Octubre", "Noviembre", "Diciembre"},
		monthsLong:    [12]string{"enero", "febrero", "marzo", "abril", "mayo", "junio", "julio", "agosto", "septiembre", "octubre", "noviembre", "diciembre"},
		monthsShort:   [12]string{"ene", "feb", "mar", "abr", "may", "jun", "jul", "ago", "sep", "oct", "nov", "dic"},
		weekdaysShort: [7]string{"dom", "lun", "mar", "mié", "jue", "vie", "sáb"},
		weekdaysLong:  [7]string{"domingo", "lunes", "martes", "miércoles", "jueves", "viernes", "sábado"},
	},
	"fr": {
		months:        [12]string{"Janvier", "Février", "Mars", "Avril", "Mai", "Juin", "Juillet", "Août", "Septembre", "Octobre", "Novembre", "Décembre"},
		monthsLong:    [12]string{"janvier", "février", "mars", "avril", "mai", "juin", "juillet", "août", "septembre", "octobre", "novembre", "décembre"},
		monthsShort:   [12]string{"jan", "fév", "mar", "avr", "mai", "jun", "jul", "aoû", "sep", "oct", "nov", "déc"},
		weekdaysShort: [7]string{"dim", "lun", "mar", "mer", "jeu", "ven", "sam"},
		weekdaysLong:  [7]string{"dimanche", "lundi", "mardi", "mercredi", "jeudi", "vendredi", "samedi"},
	},
	"ru": {
		months:        [12]string{"Январь", "Февраль", "Март", "Апрель", "Май", "Июнь", "Июль", "Август", "Сентябрь", "Октябрь", "Ноябрь", "Декабрь"},
		monthsLong:    [12]string{"января", "февраля", "марта", "апреля", "мая", "июня", "июля", "августа", "сентября", "октября", "ноября", "декабря"},
		monthsShort:   [12]string{"Янв", "Фев", "Мар", "Апр", "Май", "Июн", "Июл", "Авг", "Сен", "Окт", "Ноя", "Дек"},
		weekdaysShort: [7]string{"Вс", "Пн", "Вт", "Ср", "Чт", "Пт", "Сб"},
		weekdaysLong:  [7]string{"воскресенье", "понедельник", "вторник", "среда", "четверг", "пятница", "суббота"},
	},
	"it": {
		months:        [12]string{"Gennaio", "Febbraio", "Marzo", "Aprile", "Maggio", "Giugno", "Luglio", "Agosto", "Settembre", "Ottobre", "Novembre", "Dicembre"},
		monthsLong:    [12]string{"gennaio", "febbraio", "marzo", "aprile", "maggio", "giugno", "luglio", "agosto", "settembre", "ottobre", "novembre", "dicembre"},
		monthsShort:   [12]string{"gen", "feb", "mar", "apr", "mag", "giu", "lug", "ago", "set", "ott", "nov", "dic"},
		weekdaysShort: [7]string{"dom", "lun", "mar", "mer", "gio", "ven", "sab"},
		weekdaysLong:  [7]string{"domenica", "lunedì", "martedì", "mercoledì", "giovedì", "venerdì", "sabato"},
	},
}

func LocalizedMonthYear(language string, value time.Time) string {
	_, names := dateNamesFor(language)
	return fmt.Sprintf("%s %d", names.months[monthIndex(value)], value.Year())
}

func LocalizedDateLabel(language string, value time.Time) string {
	lang, names := dateNamesFor(language)
	weekday := names.weekdaysShort[int(value.Weekday())]
	month := names.monthsShort[monthIndex(value)]

	switch lang {
	case "ru":
		return fmt.Sprintf("%s, %d %s", weekday, value.Day(), names.monthsLong[monthIndex(value)])
	case "es":
		return fmt.Sprintf("%s, %d %s", weekday, value.Day(), month)
	case "de":
		return fmt.Sprintf("%s, %d. %s", weekday, value.Day(), month)
	case "fr", "it":
		return fmt.Sprintf("%s %d %s", weekday, value.Day(), month)
	default:
		return fmt.Sprintf("%s, %s %d", weekday, month, value.Day())
	}
}

func LocalizedDashboardDate(language string, value time.Time) string {
	lang, names := dateNamesFor(language)
	weekday := names.weekdaysLong[int(value.Weekday())]
	month := names.monthsLong[monthIndex(value)]

	switch lang {
	case "ru":
		return fmt.Sprintf("%d %s %d, %s", value.Day(), month, value.Year(), weekday)
	case "es":
		return fmt.Sprintf("%d de %s de %d, %s", value.Day(), month, value.Year(), weekday)
	case "de":
		return fmt.Sprintf("%s, %d. %s %d", weekday, value.Day(), month, value.Year())
	case "fr", "it":
		// French and Italian: "lundi 21 mars 2026" / "lunedì 21 luglio 2026"
		return fmt.Sprintf("%s %d %s %d", weekday, value.Day(), month, value.Year())
	default:
		return fmt.Sprintf("%s %d, %d, %s", month, value.Day(), value.Year(), weekday)
	}
}

func LocalizedDateDisplay(language string, value time.Time) string {
	return localizedDayMonth(language, value, true)
}

func LocalizedDateShort(language string, value time.Time) string {
	return localizedDayMonth(language, value, false)
}

// localizedDayMonth renders the compact day-month form shared by
// LocalizedDateDisplay and LocalizedDateShort — the two differ only by the
// year suffix, so the per-language ladder lives here once. Adding a seventh
// language means one new case, not two.
func localizedDayMonth(language string, value time.Time, withYear bool) string {
	if value.IsZero() {
		return ""
	}

	lang, names := dateNamesFor(language)
	if lang == "ru" {
		if withYear {
			return value.Format("02.01.2006")
		}
		return value.Format("02.01")
	}

	month := names.monthsShort[monthIndex(value)]
	switch lang {
	case "es", "fr", "it":
		if withYear {
			return fmt.Sprintf("%d %s %d", value.Day(), month, value.Year())
		}
		return fmt.Sprintf("%d %s", value.Day(), month)
	case "de":
		if withYear {
			return fmt.Sprintf("%d. %s %d", value.Day(), month, value.Year())
		}
		return fmt.Sprintf("%d. %s", value.Day(), month)
	default:
		if withYear {
			return fmt.Sprintf("%s %d, %d", month, value.Day(), value.Year())
		}
		return fmt.Sprintf("%s %d", month, value.Day())
	}
}

// dateNamesFor resolves language to a table this file actually carries — the
// language itself when it has one, English otherwise — and returns the
// normalized language alongside it, because the ladders above still switch on
// the language for word order and separators.
func dateNamesFor(language string) (string, localizedDateNames) {
	normalized := strings.ToLower(strings.TrimSpace(language))
	if names, ok := dateNames[normalized]; ok {
		return normalized, names
	}
	return "en", dateNames["en"]
}

// monthIndex converts a time to a 0-based month index. time.Month() is 1-12 for
// every time.Time (time.Date normalizes an out-of-range month into the year),
// so the result is always a valid index into a [12]string.
func monthIndex(value time.Time) int {
	return int(value.Month()) - 1
}
