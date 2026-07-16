import Foundation

enum APIError: Error {
    case invalidURL
    case invalidResponse
    case server(Int)
    case decoding
}

@MainActor
final class APIClient {
    static let shared = APIClient()

    var baseURL = URL(string: "http://127.0.0.1:8080/api/v1/")!
    var token: String?

    private let decoder: JSONDecoder = {
        let decoder = JSONDecoder()
        decoder.keyDecodingStrategy = .convertFromSnakeCase
        decoder.dateDecodingStrategy = .iso8601
        return decoder
    }()

    private let encoder: JSONEncoder = {
        let encoder = JSONEncoder()
        encoder.keyEncodingStrategy = .convertToSnakeCase
        encoder.dateEncodingStrategy = .iso8601
        return encoder
    }()

    func login(email: String, password: String) async throws -> LoginResponse {
        try await request("auth/login", method: "POST", body: LoginRequest(email: email, password: password))
    }

    func listSKUs() async throws -> ListResponse<SKU> {
        try await request("skus")
    }

    func getSKU(id: Int) async throws -> SKU {
        try await request("skus/\(id)")
    }

    func adjustInventory(skuID: Int, quantityDelta: Int, reason: String, note: String?) async throws -> InventoryAdjustment {
        let body = InventoryAdjustmentRequest(quantityDelta: quantityDelta, reason: reason, note: note)
        return try await request("skus/\(skuID)/inventory-adjustments", method: "POST", body: body)
    }

    func listSOPTemplates(category: String? = nil) async throws -> ListResponse<SOPTemplate> {
        var path = "sop-templates"
        if let category, !category.isEmpty {
            path += "?category=\(category.addingPercentEncoding(withAllowedCharacters: .urlQueryAllowed) ?? category)"
        }
        return try await request(path)
    }

    func createPhotoSession(skuID: Int, sopTemplateID: Int) async throws -> PhotoSession {
        try await request("photo-sessions", method: "POST", body: PhotoSessionRequest(skuID: skuID, sopTemplateID: sopTemplateID))
    }

    func uploadImage(
        _ imageData: Data,
        skuID: Int,
        sopViewID: Int,
        photoSessionID: Int,
        fileName: String
    ) async throws -> AssetReceipt {
        let ticket: UploadURLResponse = try await request(
            "assets/upload-url",
            method: "POST",
            body: UploadURLRequest(fileName: fileName, contentType: "image/jpeg", skuID: skuID, sopViewID: sopViewID)
        )

        guard let uploadURL = URL(string: ticket.uploadURL) else {
            throw APIError.invalidURL
        }
        var uploadRequest = URLRequest(url: uploadURL)
        uploadRequest.httpMethod = ticket.method
        for (name, value) in ticket.headers {
            uploadRequest.setValue(value, forHTTPHeaderField: name)
        }

        let (_, response) = try await URLSession.shared.upload(for: uploadRequest, from: imageData)
        guard let http = response as? HTTPURLResponse else {
            throw APIError.invalidResponse
        }
        guard (200..<300).contains(http.statusCode) else {
            throw APIError.server(http.statusCode)
        }

        return try await request(
            "assets/complete",
            method: "POST",
            body: CompleteAssetRequest(
                skuID: skuID,
                photoSessionID: photoSessionID,
                sopViewID: sopViewID,
                objectKey: ticket.objectKey,
                originalURL: ticket.assetURL,
                thumbnailURL: nil,
                capturedAt: Date()
            )
        )
    }

    private func request<Response: Decodable>(_ path: String, method: String = "GET") async throws -> Response {
        let request = try makeRequest(path: path, method: method, body: Optional<Data>.none)
        return try await send(request)
    }

    private func request<Body: Encodable, Response: Decodable>(_ path: String, method: String, body: Body) async throws -> Response {
        let data = try encoder.encode(body)
        let request = try makeRequest(path: path, method: method, body: data)
        return try await send(request)
    }

    private func makeRequest(path: String, method: String, body: Data?) throws -> URLRequest {
        guard let url = URL(string: path, relativeTo: baseURL)?.absoluteURL else {
            throw APIError.invalidURL
        }
        var request = URLRequest(url: url)
        request.httpMethod = method
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        if let token {
            request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        }
        request.httpBody = body
        return request
    }

    private func send<Response: Decodable>(_ request: URLRequest) async throws -> Response {
        let (data, response) = try await URLSession.shared.data(for: request)
        guard let http = response as? HTTPURLResponse else {
            throw APIError.invalidResponse
        }
        guard (200..<300).contains(http.statusCode) else {
            throw APIError.server(http.statusCode)
        }
        do {
            return try decoder.decode(Response.self, from: data)
        } catch {
            throw APIError.decoding
        }
    }
}
