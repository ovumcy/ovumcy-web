package services

import (
	"fmt"
	"strings"
	"time"
)

var monthNames = map[string][]string{
	"de": {"Januar", "Februar", "März", "April", "Mai", "Juni", "Juli", "August", "September", "Oktober", "November", "Dezember"},
	"en": {"January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"},
	"es": {"Enero", "Febrero", "Marzo", "Abril", "Mayo", "Junio", "Julio", "Agosto", "Septiembre", "Octubre", "Noviembre", "Diciembre"},
	"fr": {"Janvier", "Février", "Mars", "Avril", "Mai", "Juin", "Juillet", "Août", "Septembre", "Octobre", "Novembre", "Décembre"},
	"ru": {"Январь", "Февраль", "Март", "Апрель", "Май", "Июнь", "Июль", "Август", "Сентябрь", "Октябрь", "Ноябрь", "Декабрь"},
	"it": {"Gennaio", "Febbraio", "Marzo", "Aprile", "Maggio", "Giugno", "Luglio", "Agosto", "Settembre", "Ottobre", "Novembre", "Dicembre"},
}

var monthLongNames = map[string][]string{
	"de": {"Januar", "Februar", "März", "April", "Mai", "Juni", "Juli", "August", "September", "Oktober", "November", "Dezember"},
	"en": {"January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"},
	"es": {"enero", "febrero", "marzo", "abril", "mayo", "junio", "julio", "agosto", "septiembre", "octubre", "noviembre", "diciembre"},
	"fr": {"janvier", "février", "mars", "avril", "mai", "juin", "juillet", "août", "septembre", "octobre", "novembre", "décembre"},
	"ru": {"января", "февраля", "марта", "апреля", "мая", "июня", "июля", "августа", "сентября", "октября", "ноября", "декабря"},
	"it": {"gennaio", "febbraio", "marzo", "aprile", "maggio", "giugno", "luglio", "agosto", "settembre", "ottobre", "novembre", "dicembre"},
}

var weekdayShortNames = map[string][]string{
	"de": {"So.", "Mo.", "Di.", "Mi.", "Do.", "Fr.", "Sa."},
	"en": {"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"},
	"es": {"dom", "lun", "mar", "mié", "jue", "vie", "sáb"},
	"fr": {"dim", "lun", "mar", "mer", "jeu", "ven", "sam"},
	"ru": {"Вс", "Пн", "Вт", "Ср", "Чт", "Пт", "Сб"},
	"it": {"dom", "lun", "mar", "mer", "gio", "ven", "sab"},
}

var weekdayLongNames = map[string][]string{
	"de": {"Sonntag", "Montag", "Dienstag", "Mittwoch", "Donnerstag", "Freitag", "Samstag"},
	"en": {"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"},
	"es": {"domingo", "lunes", "martes", "miércoles", "jueves", "viernes", "sábado"},
	"fr": {"dimanche", "lundi", "mardi", "mercredi", "jeudi", "vendredi", "samedi"},
	"ru": {"воскресенье", "понедельник", "вторник", "среда", "четверг", "пятница", "суббота"},
	"it": {"domenica", "lunedì", "martedì", "mercoledì", "giovedì", "venerdì", "sabato"},
}

var monthShortNames = map[string][]string{
	"de": {"Jan.", "Feb.", "Mär.", "Apr.", "Mai", "Juni", "Juli", "Aug.", "Sep.", "Okt.", "Nov.", "Dez."},
	"en": {"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"},
	"es": {"ene", "feb", "mar", "abr", "may", "jun", "jul", "ago", "sep", "oct", "nov", "dic"},
	"fr": {"jan", "fév", "mar", "avr", "mai", "jun", "jul", "aoû", "sep", "oct", "nov", "déc"},
	"ru": {"Янв", "Фев", "Мар", "Апр", "Май", "Июн", "Июл", "Авг", "Сен", "Окт", "Ноя", "Дек"},
	"it": {"gen", "feb", "mar", "apr", "mag", "giu", "lug", "ago", "set", "ott", "nov", "dic"},
}

func LocalizedMonthYear(language string, value time.Time) string {
	names, ok := monthNames[dateLanguageOrDefault(language)]
	if !ok || len(names) < 12 {
		return value.Format("January 2006")
	}
	monthIndex := int(value.Month()) - 1
	if monthIndex < 0 || monthIndex >= len(names) {
		return value.Format("January 2006")
	}
	return fmt.Sprintf("%s %d", names[monthIndex], value.Year())
}

func LocalizedDateLabel(language string, value time.Time) string {
	lang := dateLanguageOrDefault(language)
	weekdays, weekdaysOK := weekdayShortNames[lang]
	months, monthsOK := monthShortNames[lang]
	if !weekdaysOK || !monthsOK {
		return value.Format("Mon, Jan 2")
	}
	monthIndex := int(value.Month()) - 1
	if monthIndex < 0 || monthIndex >= len(months) {
		return value.Format("Mon, Jan 2")
	}

	weekday := weekdays[int(value.Weekday())]
	month := months[monthIndex]
	if lang == "ru" {
		longMonths := monthLongNames[lang]
		if monthIndex < 0 || monthIndex >= len(longMonths) {
			return value.Format("Mon, Jan 2")
		}
		return fmt.Sprintf("%s, %d %s", weekday, value.Day(), longMonths[monthIndex])
	}
	if lang == "es" {
		return fmt.Sprintf("%s, %d %s", weekday, value.Day(), month)
	}
	if lang == "de" {
		return fmt.Sprintf("%s, %d. %s", weekday, value.Day(), month)
	}
	if lang == "fr" || lang == "it" {
		return fmt.Sprintf("%s %d %s", weekday, value.Day(), month)
	}
	return fmt.Sprintf("%s, %s %d", weekday, month, value.Day())
}

func LocalizedDashboardDate(language string, value time.Time) string {
	lang := dateLanguageOrDefault(language)
	weekdays, weekdaysOK := weekdayLongNames[lang]
	months, monthsOK := monthLongNames[lang]
	if !weekdaysOK || !monthsOK {
		return value.Format("January 2, 2006, Monday")
	}
	monthIndex := int(value.Month()) - 1
	if monthIndex < 0 || monthIndex >= len(months) {
		return value.Format("January 2, 2006, Monday")
	}

	weekday := weekdays[int(value.Weekday())]
	month := months[monthIndex]
	if lang == "ru" {
		return fmt.Sprintf("%d %s %d, %s", value.Day(), month, value.Year(), weekday)
	}
	if lang == "es" {
		return fmt.Sprintf("%d de %s de %d, %s", value.Day(), month, value.Year(), weekday)
	}
	if lang == "de" {
		return fmt.Sprintf("%s, %d. %s %d", weekday, value.Day(), month, value.Year())
	}
	if lang == "fr" || lang == "it" {
		// French and Italian: "lundi 21 mars 2026" / "lunedì 21 luglio 2026"
		return fmt.Sprintf("%s %d %s %d", weekday, value.Day(), month, value.Year())
	}
	return fmt.Sprintf("%s %d, %d, %s", month, value.Day(), value.Year(), weekday)
}

func LocalizedDateDisplay(language string, value time.Time) string {
	return localizedDayMonth(language, value, true)
}

func LocalizedDateShort(language string, value time.Time) string {
	return localizedDayMonth(language, value, false)
}

// localizedDayMonth renders the compact day-month form shared by
// LocalizedDateDisplay and LocalizedDateShort — the two differ only by the
// year suffix, so the per-language ladder lives here once. Adding a sixth
// language means one new case, not two.
func localizedDayMonth(language string, value time.Time, withYear bool) string {
	if value.IsZero() {
		return ""
	}

	lang := dateLanguageOrDefault(language)
	if lang == "ru" {
		if withYear {
			return value.Format("02.01.2006")
		}
		return value.Format("02.01")
	}

	months := monthShortNames[lang]
	monthIndex := int(value.Month()) - 1
	if monthIndex < 0 || monthIndex >= len(months) {
		// codecov:ignore:start -- defensive fallback: time.Month() is always
		// 1-12 for any time.Time (time.Date normalizes out-of-range months)
		// and every monthShortNames table has 12 entries, so this branch is
		// unreachable. Kept so a future table edit degrades to a stdlib
		// format instead of an index panic.
		switch {
		case lang == "en" && withYear:
			return value.Format("Jan 2, 2006")
		case lang == "en":
			return value.Format("Jan 2")
		case withYear:
			return value.Format("2 Jan 2006")
		default:
			return value.Format("2 Jan")
		}
		// codecov:ignore:end
	}
	month := months[monthIndex]

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

func dateLanguageOrDefault(language string) string {
	normalized := strings.ToLower(strings.TrimSpace(language))
	if _, ok := monthNames[normalized]; ok {
		return normalized
	}
	return "en"
}
