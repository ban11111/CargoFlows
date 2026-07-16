import SwiftUI

struct SettingsView: View {
    @EnvironmentObject private var session: SessionStore
    @EnvironmentObject private var language: LanguageStore

    var body: some View {
        NavigationStack {
            Form {
                Section(language.t("language")) {
                    Picker(language.t("language"), selection: $language.language) {
                        Text(language.t("chinese")).tag(AppLanguage.zh)
                        Text(language.t("english")).tag(AppLanguage.en)
                    }
                }

                Section(language.t("api")) {
                    LabeledContent("Base URL", value: APIClient.shared.baseURL.absoluteString)
                }
                Section(language.t("account")) {
                    Button(language.t("logout"), role: .destructive) {
                        session.logout()
                    }
                }
            }
            .navigationTitle(language.t("tab.settings"))
        }
    }
}
