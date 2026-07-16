import SwiftUI

@main
struct CargoFlowApp: App {
    @StateObject private var session = SessionStore()
    @StateObject private var language = LanguageStore()

    var body: some Scene {
        WindowGroup {
            RootView()
                .environmentObject(session)
                .environmentObject(language)
        }
    }
}
