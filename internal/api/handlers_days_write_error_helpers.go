package api

func upsertDayPersistenceErrorSpec(err error) APIErrorSpec {
	return mapDayUpsertError(err)
}

func deleteDayPersistenceErrorSpec(err error) APIErrorSpec {
	return mapDayDeleteError(err)
}
