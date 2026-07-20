import SwiftUI

struct LoginView: View {
    @EnvironmentObject private var session: SessionStore
    @EnvironmentObject private var language: LanguageStore
    @State private var email = "admin@cargoflows.cc"
    @State private var password = "password123"
    @State private var isSubmitting = false

    var body: some View {
        NavigationStack {
            ZStack {
                CargoTheme.canvas.ignoresSafeArea()
                ScrollView {
                    VStack(alignment: .leading, spacing: 28) {
                        VStack(alignment: .leading, spacing: 20) {
                            CargoBrandMark()
                            VStack(alignment: .leading, spacing: 8) {
                                Text(language.t("login.section"))
                                    .font(.subheadline.weight(.semibold))
                                    .foregroundStyle(CargoTheme.accent)
                                Text(language.t("login.title"))
                                    .font(.largeTitle.weight(.bold))
                                Text(language.t("login.subtitle"))
                                    .font(.body)
                                    .foregroundStyle(.secondary)
                                    .fixedSize(horizontal: false, vertical: true)
                            }
                        }

                        CargoPanel {
                            VStack(spacing: 16) {
                                Label {
                                    TextField(language.t("login.email"), text: $email)
                                        .cargoNoAutocapitalization()
                                        .cargoEmailKeyboard()
                                        .textContentType(.username)
                                } icon: {
                                    Image(systemName: "envelope")
                                        .foregroundStyle(CargoTheme.accent)
                                }
                                Divider()
                                Label {
                                    SecureField(language.t("login.password"), text: $password)
                                        .textContentType(.password)
                                } icon: {
                                    Image(systemName: "lock")
                                        .foregroundStyle(CargoTheme.accent)
                                }
                            }
                            .frame(minHeight: 96)
                        }

                        if let errorKey = session.errorKey {
                            Label(language.t(errorKey), systemImage: "exclamationmark.triangle.fill")
                                .font(.callout)
                                .foregroundStyle(.red)
                                .padding(16)
                                .frame(maxWidth: .infinity, alignment: .leading)
                                .background(Color.red.opacity(0.09), in: RoundedRectangle(cornerRadius: 16, style: .continuous))
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
                                Spacer()
                                if isSubmitting {
                                    ProgressView().tint(.white)
                                } else {
                                    Image(systemName: "arrow.right")
                                }
                            }
                        }
                        .buttonStyle(CargoPrimaryButtonStyle())
                        .disabled(isSubmitting || email.isEmpty || password.isEmpty)
                        .opacity((isSubmitting || email.isEmpty || password.isEmpty) ? 0.55 : 1)
                    }
                    .padding(.horizontal, 24)
                    .padding(.vertical, 36)
                    .frame(maxWidth: 560)
                    .frame(maxWidth: .infinity)
                }
            }
            .cargoInlineNavigationTitle()
        }
    }
}
