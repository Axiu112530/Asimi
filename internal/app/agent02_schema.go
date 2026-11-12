package app

// The task JSON and existing step/action relations hold plan versions and
// usage reservations. Only the independent desktop/screen links need v6 DDL.
const schemaV6 = schemaAgent02Computer + `
PRAGMA user_version=6;`
