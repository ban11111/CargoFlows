import XCTest
@testable import CargoFlow

final class SOPDTOTests: XCTestCase {
    func testDecodesCurrentPublicSKUPayload() throws {
        let data = Data(#"{"public_id":"11111111-1111-1111-1111-111111111111","code":"CF-001","color":"Navy","size":"M","barcode":"123","stock":8,"low_stock_threshold":3,"platform_title":"CargoFlow Case","selling_points":"Durable","status":"active","created_at":"2026-07-16T00:00:00Z","updated_at":"2026-07-16T00:00:00Z","product":{"category_id":42,"name":"Travel Case","brand":"CargoFlow","category":"Cases","description":"A case","category_record":{"id":42,"name":"箱包","name_en":"Cases","is_system":true,"created_at":"2026-07-16T00:00:00Z","updated_at":"2026-07-16T00:00:00Z"}},"tags":[{"name":"featured"}]}"#.utf8)

        let sku = try JSONDecoder().decode(SKU.self, from: data)

        XCTAssertEqual(sku.id, "11111111-1111-1111-1111-111111111111")
        XCTAssertEqual(sku.product.categoryID, 42)
        XCTAssertEqual(sku.tags.map(\.name), ["featured"])
        XCTAssertEqual(sku.stock, 8)
    }

    func testDecodesPackagingFrontAndLocalizesName() throws {
        let data = Data(#"{"public_id":"11111111-1111-1111-1111-111111111111","sequence":2,"role":"capture","view_kind":"standard","preset_key":"packaging_front","name":{"zh-CN":"包装正面","en":"Packaging Front"},"instruction":{"zh-CN":"拍摄包装","en":"Capture package"},"required":false,"pose":{"space":"object","camera_position_direction":[0,0,1],"image_up_direction":[1,0,0],"target":[0,0,0]},"composition":{"frame_occupancy":0.85,"aspect_ratio":"1:1","allow_rotation_correction":true,"allow_mirror":false},"reference_images":[]}"#.utf8)

        let view = try JSONDecoder().decode(SOPView.self, from: data)

        XCTAssertEqual(view.id, "11111111-1111-1111-1111-111111111111")
        XCTAssertEqual(view.displayName(for: .zh), "包装正面")
        XCTAssertEqual(view.displayName(for: .en), "Packaging Front")
        XCTAssertFalse(view.required)
        XCTAssertEqual(view.pose.cameraPositionDirection, Vector3DTO(x: 0, y: 0, z: 1))
    }

    func testLocalizedTextFallsBackToOtherNonemptyLanguage() throws {
        let chineseMissing = try JSONDecoder().decode(LocalizedText.self, from: Data(#"{"zh-CN":"","en":"Front"}"#.utf8))
        let englishMissing = try JSONDecoder().decode(LocalizedText.self, from: Data(#"{"zh-CN":"正面","en":""}"#.utf8))

        XCTAssertEqual(chineseMissing.value(for: .zh), "Front")
        XCTAssertEqual(englishMissing.value(for: .en), "正面")
    }

    func testVectorRejectsWrongCardinalityAndNonfiniteValues() throws {
        XCTAssertThrowsError(try JSONDecoder().decode(Vector3DTO.self, from: Data("[1,2]".utf8)))
        XCTAssertThrowsError(try JSONDecoder().decode(Vector3DTO.self, from: Data("[1,2,3,4]".utf8)))
        XCTAssertThrowsError(try JSONDecoder().decode(Vector3DTO.self, from: Data("[1,2,1e400]".utf8)))
    }
}

@MainActor
final class SOPAPIClientTests: XCTestCase {
    override func setUp() {
        super.setUp()
        URLProtocolStub.reset()
    }

    func testPublishedListVersionAndSessionContracts() async throws {
        URLProtocolStub.handler = { request in
            switch request.url.flatMap({ URLComponents(url: $0, resolvingAgainstBaseURL: false)?.percentEncodedPath }) {
            case "/api/v1/capture-sops":
                XCTAssertEqual(request.url?.query, "category_id=42")
                return Self.response(request, body: #"{"data":[]}"#)
            case "/api/v1/sop-versions/version%2Fwith%20space":
                return Self.response(request, body: Self.versionJSON)
            case "/api/v1/photo-sessions":
                XCTAssertEqual(request.httpMethod, "POST")
                XCTAssertEqual(try Self.jsonBody(request), ["sku_id": "sku-id", "sop_version_id": "version-id"])
                return Self.response(request, status: 201, body: #"{"public_id":"session-id","code":"PS-1","sku_id":"sku-id","sop_version_id":"version-id","status":"in_progress","created_at":"2026-07-16T00:00:00Z"}"#)
            default:
                XCTFail("Unexpected request: \(request.url?.absoluteString ?? "nil")")
                return Self.response(request, status: 404, body: "{}")
            }
        }
        let client = makeClient()

        let summaries = try await client.listPublishedSOPs(categoryID: 42)
        XCTAssertTrue(summaries.data.isEmpty)
        let version = try await client.getSOPVersion(id: "version/with space")
        XCTAssertEqual(version.id, "version-id")
        let session = try await client.createPhotoSession(skuID: "sku-id", sopVersionID: "version-id")
        XCTAssertEqual(session.id, "session-id")
    }

    func testUploadUsesUUIDTicketAndCompletionBodies() async throws {
        URLProtocolStub.handler = { request in
            switch request.url?.host {
            case "example.test":
                XCTAssertEqual(request.httpMethod, "PUT")
                XCTAssertEqual(request.value(forHTTPHeaderField: "Content-Type"), "image/jpeg")
                return Self.response(request, body: "")
            default:
                switch request.url?.path {
                case "/api/v1/assets/upload-url":
                    XCTAssertEqual(try Self.jsonBody(request), [
                        "file_name": "capture.jpg",
                        "content_type": "image/jpeg",
                        "photo_session_id": "session-id",
                        "sop_view_id": "view-id",
                    ])
                    return Self.response(request, body: #"{"method":"PUT","upload_url":"https://example.test/upload","completion_token":"signed-ticket","expires_in":900,"headers":{"Content-Type":"image/jpeg"}}"#)
                case "/api/v1/assets/complete":
                    let body = try Self.jsonBody(request)
                    XCTAssertEqual(body["photo_session_id"] as? String, "session-id")
                    XCTAssertEqual(body["sop_view_id"] as? String, "view-id")
                    XCTAssertEqual(body["completion_token"] as? String, "signed-ticket")
                    XCTAssertNil(body["sku_id"])
                    XCTAssertNil(body["object_key"])
                    XCTAssertNil(body["original_url"])
                    XCTAssertNil(body["thumbnail_url"])
                    return Self.response(request, status: 201, body: #"{"public_id":"asset-id","sku_id":"sku-id","photo_session_id":"session-id","sop_view_id":"view-id","media_url":"/api/v1/assets/asset-id/media","review_status":"pending","captured_at":"2026-07-16T00:00:00Z"}"#)
                default:
                    XCTFail("Unexpected request: \(request.url?.absoluteString ?? "nil")")
                    return Self.response(request, status: 404, body: "{}")
                }
            }
        }

        let receipt = try await makeClient().uploadImage(
            Data([1, 2, 3]),
            sopViewID: "view-id",
            photoSessionID: "session-id",
            fileName: "capture.jpg"
        )
        XCTAssertEqual(receipt.publicID, "asset-id")
        XCTAssertEqual(receipt.photoSessionID, "session-id")
    }

    func testListsSKUHistoryAndLoadsAuthenticatedMedia() async throws {
        URLProtocolStub.handler = { request in
            XCTAssertEqual(request.value(forHTTPHeaderField: "Authorization"), "Bearer test-token")
            switch request.url?.path {
            case "/api/v1/assets/review":
                XCTAssertEqual(request.url?.query, "sku_id=sku-id")
                return Self.response(request, body: #"{"data":[{"public_id":"asset-id","sku_id":"sku-id","media_url":"/api/v1/assets/asset-id/media","review_status":"rejected","captured_at":"2026-07-16T00:00:00Z","sop_view_id":"view-id","sop_view_key":"reference_front","sop_view_name":{"zh-CN":"正面","en":"Front"},"photo_session_code":"PS-1"}]}"#)
            case "/api/v1/assets/asset-id/media":
                return Self.response(request, body: "image-bytes")
            default:
                XCTFail("Unexpected request: \(request.url?.absoluteString ?? "nil")")
                return Self.response(request, status: 404, body: "{}")
            }
        }
        let client = makeClient()
        client.token = "test-token"

        let history = try await client.listAssets(skuID: "sku-id")
        let asset = try XCTUnwrap(history.data.first)
        XCTAssertEqual(asset.reviewStatus, "rejected")
        XCTAssertEqual(asset.sopViewID, "view-id")
        XCTAssertEqual(asset.sopViewKey, "reference_front")
        XCTAssertEqual(asset.sopViewName.value(for: .zh), "正面")
        let media = try await client.loadAssetMedia(asset.mediaURL)
        XCTAssertEqual(media, Data("image-bytes".utf8))
    }

    private func makeClient() -> APIClient {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [URLProtocolStub.self]
        return APIClient(baseURL: URL(string: "https://api.test/api/v1/")!, session: URLSession(configuration: configuration))
    }

    nonisolated private static func response(_ request: URLRequest, status: Int = 200, body: String) -> (HTTPURLResponse, Data) {
        (HTTPURLResponse(url: request.url!, statusCode: status, httpVersion: nil, headerFields: nil)!, Data(body.utf8))
    }

    nonisolated private static func jsonBody(_ request: URLRequest) throws -> [String: AnyHashable] {
        let data: Data
        if let body = request.httpBody {
            data = body
        } else if let stream = request.httpBodyStream {
            stream.open()
            defer { stream.close() }
            var body = Data()
            let buffer = UnsafeMutablePointer<UInt8>.allocate(capacity: 4096)
            defer { buffer.deallocate() }
            while stream.hasBytesAvailable {
                let count = stream.read(buffer, maxLength: 4096)
                guard count >= 0 else { throw stream.streamError ?? APIError.invalidResponse }
                if count == 0 { break }
                body.append(buffer, count: count)
            }
            data = body
        } else {
            throw APIError.invalidResponse
        }
        return try XCTUnwrap(JSONSerialization.jsonObject(with: data) as? [String: AnyHashable])
    }

    nonisolated private static let versionJSON = #"{"schema_version":"1.0","public_id":"version-id","sop_public_id":"sop-id","version_number":1,"status":"published","name":{"zh-CN":"规范","en":"SOP"},"description":{"zh-CN":"","en":""},"coordinate_system":{"id":"pcs_object_v1","handedness":"right_handed","origin":"bounding_box_center","unit":"normalized","axes":{"x_positive":"object_top","y_positive":"object_left","z_positive":"object_front"}},"published_at":"2026-07-16T00:00:00Z","created_at":"2026-07-16T00:00:00Z","updated_at":"2026-07-16T00:00:00Z","views":[]}"#
}

private final class URLProtocolStub: URLProtocol, @unchecked Sendable {
    nonisolated(unsafe) static var handler: (@Sendable (URLRequest) throws -> (HTTPURLResponse, Data))?

    static func reset() {
        handler = nil
    }

    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }

    override func startLoading() {
        do {
            let (response, data) = try XCTUnwrap(Self.handler)(request)
            client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
            client?.urlProtocol(self, didLoad: data)
            client?.urlProtocolDidFinishLoading(self)
        } catch {
            client?.urlProtocol(self, didFailWithError: error)
        }
    }

    override func stopLoading() {}
}
