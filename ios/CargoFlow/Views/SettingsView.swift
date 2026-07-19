import SwiftUI

struct SettingsView: View {
    @EnvironmentObject private var session: SessionStore
    @EnvironmentObject private var language: LanguageStore

    var body: some View {
        NavigationStack {
            Form {
                Section {
                    HStack(spacing: 14) {
                        CargoBrandMark()
                        VStack(alignment: .leading, spacing: 3) {
                            Text("CargoFlow").font(.headline)
                            Label(language.t("connection.ready"), systemImage: "checkmark.circle.fill")
                                .font(.caption)
                                .foregroundStyle(.green)
                        }
                    }
                    .padding(.vertical, 6)
                }
                Section(language.t("language")) {
                    Picker(language.t("language"), selection: $language.language) {
                        Text(language.t("chinese")).tag(AppLanguage.zh)
                        Text(language.t("english")).tag(AppLanguage.en)
                    }
                }

                Section(language.t("api")) {
                    LabeledContent("Base URL") {
                        Text(APIClient.shared.baseURL.host ?? APIClient.shared.baseURL.absoluteString)
                            .foregroundStyle(.secondary)
                    }
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
