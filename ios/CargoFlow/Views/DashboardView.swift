import SwiftUI

struct DashboardView: View {
    @EnvironmentObject private var language: LanguageStore

    var body: some View {
        TabView {
            SKUListView()
                .tabItem {
                    Label(language.t("tab.sku"), systemImage: "shippingbox")
                }

            CaptureHomeView()
                .tabItem {
                    Label(language.t("tab.capture"), systemImage: "camera.viewfinder")
                }

            SettingsView()
                .tabItem {
                    Label(language.t("tab.settings"), systemImage: "gearshape")
                }
        }
    }
}
