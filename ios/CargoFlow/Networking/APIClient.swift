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

    var baseURL: URL
    var token: String?
    private let session: URLSession

    private let decoder: JSONDecoder = {
        let decoder = JSONDecoder()
        decoder.dateDecodingStrategy = .iso8601
        return decoder
    }()

    private let encoder: JSONEncoder = {
        let encoder = JSONEncoder()
        encoder.dateEncodingStrategy = .iso8601
        return encoder
    }()

    init(
        baseURL: URL = URL(string: "http://127.0.0.1:8080/api/v1/")!,
        session: URLSession = .shared
    ) {
        self.baseURL = baseURL
        self.session = session
    }

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

    func listPublishedSOPs(categoryID: Int) async throws -> ListResponse<CaptureSOPSummary> {
        try await request("capture-sops?category_id=\(categoryID)")
    }

    func getSOPVersion(id: String) async throws -> SOPVersion {
        try await request("sop-versions/\(encodedPathSegment(id))")
    }

    func createPhotoSession(skuID: Int, sopVersionID: String) async throws -> PhotoSession {
        try await request("photo-sessions", method: "POST", body: PhotoSessionRequest(skuID: skuID, sopVersionID: sopVersionID))
    }

    func uploadImage(
        _ imageData: Data,
        skuID: Int,
        sopViewID: String,
        photoSessionID: String,
        fileName: String
    ) async throws -> AssetReceipt {
        _ = skuID
        let ticket: UploadURLResponse = try await request(
            "assets/upload-url",
            method: "POST",
            body: UploadURLRequest(
                fileName: fileName,
                contentType: "image/jpeg",
                photoSessionID: photoSessionID,
                sopViewID: sopViewID
            )
        )

        guard let uploadURL = URL(string: ticket.uploadURL) else {
            throw APIError.invalidURL
        }
        var uploadRequest = URLRequest(url: uploadURL)
        uploadRequest.httpMethod = ticket.method
        for (name, value) in ticket.headers {
            uploadRequest.setValue(value, forHTTPHeaderField: name)
        }

        let (_, response) = try await session.upload(for: uploadRequest, from: imageData)
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
                photoSessionID: photoSessionID,
                sopViewID: sopViewID,
                completionToken: ticket.completionToken,
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

    private func encodedPathSegment(_ value: String) -> String {
        var allowed = CharacterSet.alphanumerics
        allowed.insert(charactersIn: "-._~")
        return value.addingPercentEncoding(withAllowedCharacters: allowed) ?? value
    }

    private func send<Response: Decodable>(_ request: URLRequest) async throws -> Response {
        let (data, response) = try await session.data(for: request)
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
