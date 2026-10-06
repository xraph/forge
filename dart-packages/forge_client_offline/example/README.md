# forge_client_offline_example

A macOS app that exists to run `integration_test/keystore_test.dart` against the real login Keychain and the sqlite3mc build that Flutter links into a real app.

    flutter test integration_test/keystore_test.dart -d macos

The test uses the login keychain (`useDataProtectionKeychain: false`) because the data protection keychain needs a signing team and the `keychain-access-groups` entitlement.
