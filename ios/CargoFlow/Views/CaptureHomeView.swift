import SwiftUI

struct CaptureHomeView: View {
    @EnvironmentObject private var language: LanguageStore

    var body: some View {
        NavigationStack {
            ZStack {
                CargoTheme.canvas.ignoresSafeArea()
                VStack(spacing: 24) {
                    ZStack {
                        Circle()
                            .fill(CargoTheme.accent.opacity(0.11))
                            .frame(width: 112, height: 112)
                        Image(systemName: "camera.viewfinder")
                            .font(.system(size: 42, weight: .medium))
                            .foregroundStyle(CargoTheme.accent)
                    }
                    VStack(spacing: 10) {
                        Text(language.t("capture.empty.title"))
                            .font(.title2.weight(.bold))
                        Text(language.t("capture.empty.desc"))
                            .font(.body)
                            .foregroundStyle(.secondary)
                            .multilineTextAlignment(.center)
                            .frame(maxWidth: 360)
                    }
                    Label(language.t("capture.empty.action"), systemImage: "shippingbox")
                        .font(.callout.weight(.semibold))
                        .foregroundStyle(CargoTheme.accent)
                }
                .padding(32)
            }
            .navigationTitle(language.t("capture"))
        }
    }
}
