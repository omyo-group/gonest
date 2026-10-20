// Package settings holds the typed configuration of the SaaS example. It is a
// leaf package so every feature module can inject *settings.Config without
// import cycles; config.ForRoot in package app makes it globally visible.
package settings
