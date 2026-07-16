import Foundation

@MainActor
final class SessionStore: ObservableObject {
    @Published private(set) var isAuthenticated = false
    @Published var currentUser: User?
    @Published var errorKey: String?

    private let tokenKey = "cargo_flow_token"
    private let api = APIClient.shared

    func restore() {
        if let token = UserDefaults.standard.string(forKey: tokenKey), !token.isEmpty {
            api.token = token
            isAuthenticated = true
        }
    }

    func login(email: String, password: String) async {
        errorKey = nil
        do {
            let response = try await api.login(email: email, password: password)
            api.token = response.token
            currentUser = response.user
            UserDefaults.standard.set(response.token, forKey: tokenKey)
            isAuthenticated = true
        } catch {
            errorKey = "login.error"
        }
    }

    func logout() {
        api.token = nil
        currentUser = nil
        UserDefaults.standard.removeObject(forKey: tokenKey)
        isAuthenticated = false
    }
}
