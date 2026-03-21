package services

import (
	"fmt"
	"strings"
	"time"
)

var monthNames = map[string][]string{
	"en": {"January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"},
	"es": {"Enero", "Febrero", "Marzo", "Abril", "Mayo", "Junio", "Julio", "Agosto", "Septiembre", "Octubre", "Noviembre", "Diciembre"},
	"fr": {"Janvier", "Février", "Mars", "Avril", "Mai", "Juin", "Juillet", "Août", "Septembre", "Octobre", "Novembre", "Décembre"},
	"ru": {"Январь", "Февраль", "Март", "Апрель", "Май", "Июнь", "Июль", "Август", "Сентябрь", "Октябрь", "Ноябрь", "Декабрь"},
}

var monthLongNames = map[string][]string{
	"en": {"January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"},
	"es": {"enero", "febrero", "marzo", "abril", "mayo", "junio", "julio", "agosto", "septiembre", "octubre", "noviembre", "diciembre"},
	"fr": {"janvier", "février", "mars", "avril", "mai", "juin", "juillet", "août", "septembre", "octobre", "novembre", "décembre"},
	"ru": {"января", "февраля", "марта", "апреля", "мая", "июня", "июля", "августа", "сентября", "октября", "ноября", "декабря"},
}

var weekdayShortNames = map[string][]string{
	"en": {"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"},
	"es": {"dom", "lun", "mar", "mié", "jue", "vie", "sáb"},
	"fr": {"dim", "lun", "mar", "mer", "jeu", "ven", "sam"},
	"ru": {"Вс", "Пн", "Вт", "Ср", "Чт", "Пт", "Сб"},
}

var weekdayLongNames = map[string][]string{
	"en": {"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"},
	"es": {"domingo", "lunes", "martes", "miércoles", "jueves", "viernes", "sábado"},
	"fr": {"dimanche", "lundi", "mardi", "mercredi", "jeudi", "vendredi", "samedi"},
	"ru": {"воскресенье", "понедельник", "вторник", "среда", "четверг", "пятница", "суббота"},
}

var monthShortNames = map[string][]string{
	"en": {"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"},
	"es": {"ene", "feb", "mar", "abr", "may", "jun", "jul", "ago", "sep", "oct", "nov", "dic"},
	"fr": {"jan", "fév", "mar", "avr", "mai", "jun", "jul", "aoû", "sep", "oct", "nov", "déc"},
	"ru": {"Янв", "Фев", "Мар", "Апр", "Май", "Июн", "Июл", "Авг", "Сен", "Окт", "Ноя", "Дек"},
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
	if lang == "fr" {
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
	if lang == "fr" {
		// French: "lundi 21 mars 2026"
		return fmt.Sprintf("%s %d %s %d", weekday, value.Day(), month, value.Year())
	}
	return fmt.Sprintf("%s %d, %d, %s", month, value.Day(), value.Year(), weekday)
}

func LocalizedDateDisplay(language string, value time.Time) string {
	if value.IsZero() {
		return ""
	}

	lang := dateLanguageOrDefault(language)
	if lang == "ru" {
		return value.Format("02.01.2006")
	}
	if lang == "es" {
		months := monthShortNames[lang]
		monthIndex := int(value.Month()) - 1
		if monthIndex < 0 || monthIndex >= len(months) {
			return value.Format("2 Jan 2006")
		}
		return fmt.Sprintf("%d %s %d", value.Day(), months[monthIndex], value.Year())
	}
	if lang == "fr" {
		months := monthShortNames[lang]
		monthIndex := int(value.Month()) - 1
		if monthIndex < 0 || monthIndex >= len(months) {
			return value.Format("2 Jan 2006")
		}
		// French: "21 mar 2026"
		return fmt.Sprintf("%d %s %d", value.Day(), months[monthIndex], value.Year())
	}

	months := monthShortNames["en"]
	monthIndex := int(value.Month()) - 1
	if monthIndex < 0 || monthIndex >= len(months) {
		return value.Format("Jan 2, 2006")
	}
	return fmt.Sprintf("%s %d, %d", months[monthIndex], value.Day(), value.Year())
}

func LocalizedDateShort(language string, value time.Time) string {
	if value.IsZero() {
		return ""
	}

	lang := dateLanguageOrDefault(language)
	if lang == "ru" {
		return value.Format("02.01")
	}
	if lang == "es" {
		months := monthShortNames[lang]
		monthIndex := int(value.Month()) - 1
		if monthIndex < 0 || monthIndex >= len(months) {
			return value.Format("2 Jan")
		}
		return fmt.Sprintf("%d %s", value.Day(), months[monthIndex])
	}
	if lang == "fr" {
		months := monthShortNames[lang]
		monthIndex := int(value.Month()) - 1
		if monthIndex < 0 || monthIndex >= len(months) {
			return value.Format("2 Jan")
		}
		// French: "21 mar"
		return fmt.Sprintf("%d %s", value.Day(), months[monthIndex])
	}

	months := monthShortNames["en"]
	monthIndex := int(value.Month()) - 1
	if monthIndex < 0 || monthIndex >= len(months) {
		return value.Format("Jan 2")
	}
	return fmt.Sprintf("%s %d", months[monthIndex], value.Day())
}

func dateLanguageOrDefault(language string) string {
	normalized := strings.ToLower(strings.TrimSpace(language))
	if _, ok := monthNames[normalized]; ok {
		return normalized
	}
	return "en"
}
