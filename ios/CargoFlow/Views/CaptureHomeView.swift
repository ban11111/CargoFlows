import SwiftUI

struct CaptureHomeView: View {
    @EnvironmentObject private var language: LanguageStore

    var body: some View {
        NavigationStack {
            ContentUnavailableView(
                language.t("capture.empty.title"),
                systemImage: "camera.viewfinder",
                description: Text(language.t("capture.empty.desc"))
            )
            .navigationTitle(language.t("capture"))
        }
    }
}
