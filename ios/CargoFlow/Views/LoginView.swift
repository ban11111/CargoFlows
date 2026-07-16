import SwiftUI

struct LoginView: View {
    @EnvironmentObject private var session: SessionStore
    @EnvironmentObject private var language: LanguageStore
    @State private var email = "admin@cargoflow.local"
    @State private var password = "password123"
    @State private var isSubmitting = false

    var body: some View {
        NavigationStack {
            ScrollView {
                VStack(alignment: .leading, spacing: 24) {
                    VStack(alignment: .leading, spacing: 10) {
                        Text(language.t("login.title"))
                            .font(.largeTitle.bold())
                            .lineLimit(2)
                            .minimumScaleFactor(0.8)
                        Text(language.t("login.section"))
                            .font(.title3.weight(.semibold))
                            .foregroundStyle(.secondary)
                    }

                    VStack(spacing: 0) {
                        TextField(language.t("login.email"), text: $email)
                            .cargoNoAutocapitalization()
                            .cargoEmailKeyboard()
                            .textContentType(.username)
                            .padding(.vertical, 14)
                        Divider()
                        SecureField(language.t("login.password"), text: $password)
                            .textContentType(.password)
                            .padding(.vertical, 14)
                    }
                    .padding(.horizontal, 18)
                    .background(.background, in: RoundedRectangle(cornerRadius: 16))

                    if let errorKey = session.errorKey {
                        HStack(alignment: .top, spacing: 10) {
                            Image(systemName: "exclamationmark.triangle.fill")
                                .font(.body.weight(.semibold))
                                .foregroundStyle(.red)
                            Text(language.t(errorKey))
                                .foregroundStyle(.red)
                                .lineLimit(nil)
                                .fixedSize(horizontal: false, vertical: true)
                        }
                        .padding(16)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .background(.background, in: RoundedRectangle(cornerRadius: 16))
                        .accessibilityElement(children: .combine)
                    }

                    Button {
                        Task {
                            isSubmitting = true
                            await session.login(email: email, password: password)
                            isSubmitting = false
                        }
                    } label: {
                        HStack {
                            Text(isSubmitting ? language.t("login.submitting") : language.t("login.submit"))
                                .font(.headline)
                            Spacer()
                            if isSubmitting {
                                ProgressView()
                            } else {
                                Image(systemName: "arrow.right")
                            }
                        }
                        .padding(.horizontal, 18)
                        .frame(maxWidth: .infinity)
                        .frame(height: 56)
                    }
                    .buttonStyle(.borderedProminent)
                    .disabled(isSubmitting)
                }
                .padding(.horizontal, 24)
                .padding(.vertical, 40)
            }
            .background(Color(red: 0.95, green: 0.95, blue: 0.97))
            .cargoInlineNavigationTitle()
        }
    }
}
