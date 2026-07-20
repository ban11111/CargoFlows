import SwiftUI

@main
struct CargoFlowsApp: App {
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
