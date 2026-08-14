package models

import "time"

type SymptomType struct {
	ID         uint       `gorm:"primaryKey"`
	UserID     uint       `gorm:"not null;index"`
	Name       string     `gorm:"not null"`
	Icon       string     `gorm:"not null"`
	Color      string     `gorm:"not null"`
	IsBuiltin  bool       `gorm:"not null;default:false"`
	ArchivedAt *time.Time `gorm:"index"`
}

type BuiltinSymptom struct {
	Key            string
	Name           string
	TranslationKey string
	Icon           string
	Color          string
}

var defaultBuiltinSymptoms = []BuiltinSymptom{
	{Key: "cramps", Name: "Cramps", TranslationKey: "symptoms.cramps", Icon: "🩸", Color: "#FF4444"},
	{Key: "headache", Name: "Headache", TranslationKey: "symptoms.headache", Icon: "🤕", Color: "#FFA500"},
	{Key: "mood_swings", Name: "Mood swings", TranslationKey: "symptoms.mood_swings", Icon: "😢", Color: "#9B59B6"},
	{Key: "bloating", Name: "Bloating", TranslationKey: "symptoms.bloating", Icon: "🎈", Color: "#3498DB"},
	{Key: "fatigue", Name: "Fatigue", TranslationKey: "symptoms.fatigue", Icon: "😴", Color: "#95A5A6"},
	{Key: "breast_tenderness", Name: "Breast tenderness", TranslationKey: "symptoms.breast_tenderness", Icon: "💔", Color: "#E91E63"},
	{Key: "acne", Name: "Acne", TranslationKey: "symptoms.acne", Icon: "🔴", Color: "#E74C3C"},
	{Key: "back_pain", Name: "Back pain", TranslationKey: "symptom.back_pain", Icon: "🦴", Color: "#8E6E53"},
	{Key: "nausea", Name: "Nausea", TranslationKey: "symptom.nausea", Icon: "🤢", Color: "#7CB342"},
	{Key: "spotting", Name: "Spotting", TranslationKey: "symptom.spotting", Icon: "🩹", Color: "#C55A7A"},
	{Key: "irritability", Name: "Irritability", TranslationKey: "symptom.irritability", Icon: "😤", Color: "#FF7043"},
	{Key: "insomnia", Name: "Insomnia", TranslationKey: "symptom.insomnia", Icon: "🌙", Color: "#5C6BC0"},
	{Key: "food_cravings", Name: "Food cravings", TranslationKey: "symptom.food_cravings", Icon: "🍫", Color: "#A1887F"},
	{Key: "diarrhea", Name: "Diarrhea", TranslationKey: "symptom.diarrhea", Icon: "🚽", Color: "#26A69A"},
	{Key: "constipation", Name: "Constipation", TranslationKey: "symptom.constipation", Icon: "🪨", Color: "#8D6E63"},
	{Key: "swelling", Name: "Swelling", TranslationKey: "symptom.swelling", Icon: "💧", Color: "#64B5F6"},
}

func DefaultBuiltinSymptoms() []BuiltinSymptom {
	symptoms := make([]BuiltinSymptom, len(defaultBuiltinSymptoms))
	copy(symptoms, defaultBuiltinSymptoms)
	return symptoms
}

func (symptom SymptomType) IsActive() bool {
	return symptom.ArchivedAt == nil
}
