import XCTest
@testable import CargoFlow

@MainActor
final class SOPCaptureLogicTests: XCTestCase {
    func testOptionalPackagingDoesNotBlockFinish() throws {
        let views = try [viewFixture(id: "reference", required: true), viewFixture(id: "packaging", required: false)]

        XCTAssertTrue(requiredViewsComplete(views: views, capturedViewIDs: ["reference"]))
    }

    func testEveryRequiredViewMustBeCaptured() throws {
        let views = try [
            viewFixture(id: "reference", required: true),
            viewFixture(id: "back", required: true),
            viewFixture(id: "packaging", required: false),
        ]

        XCTAssertFalse(requiredViewsComplete(views: views, capturedViewIDs: ["reference", "packaging"]))
        XCTAssertTrue(requiredViewsComplete(views: views, capturedViewIDs: ["reference", "back"]))
    }

    func testPublishedCandidatesAreFilteredAndSortedDeterministically() throws {
        let summaries = try [
            summaryFixture(sopID: "sop-b", versions: [("b-v1", 1, "published"), ("b-draft", 2, "draft")]),
            summaryFixture(sopID: "sop-a", versions: [("a-v1", 1, "published"), ("a-v3", 3, "published")]),
        ]

        XCTAssertEqual(publishedVersionCandidates(from: summaries).map(\.id), ["a-v3", "a-v1", "b-v1"])
    }

    func testChangingVersionRequiresConfirmationAndThenResetsCaptureAndSession() async throws {
        let state = SOPCaptureState()
        state.installCandidates([
            SOPVersionCandidate(id: "v1", sopID: "sop", versionNumber: 1, name: LocalizedText(zhCN: "一", en: "One")),
            SOPVersionCandidate(id: "v2", sopID: "sop", versionNumber: 2, name: LocalizedText(zhCN: "二", en: "Two")),
        ])
        state.recordCapture(viewID: "view-1")
        _ = try await state.resolveSession { self.sessionFixture(versionID: $0) }

        XCTAssertEqual(state.requestSelection("v2"), .needsConfirmation)
        XCTAssertEqual(state.selectedVersionID, "v1")
        XCTAssertEqual(state.capturedViewIDs, ["view-1"])

        state.confirmSelection("v2")

        XCTAssertEqual(state.selectedVersionID, "v2")
        XCTAssertTrue(state.capturedViewIDs.isEmpty)
        XCTAssertNil(state.session)
    }

    func testStaleVersionLoadCannotReplaceNewerSelection() throws {
        let state = SOPCaptureState()
        state.installCandidates([
            SOPVersionCandidate(id: "v1", sopID: "sop", versionNumber: 1, name: LocalizedText(zhCN: "一", en: "One")),
            SOPVersionCandidate(id: "v2", sopID: "sop", versionNumber: 2, name: LocalizedText(zhCN: "二", en: "Two")),
        ])
        let staleToken = try XCTUnwrap(state.beginVersionLoad())
        XCTAssertEqual(state.requestSelection("v2"), .changed)
        let currentToken = try XCTUnwrap(state.beginVersionLoad())

        XCTAssertFalse(state.accept(version: try versionFixture(id: "v1", number: 1), token: staleToken))
        XCTAssertTrue(state.accept(version: try versionFixture(id: "v2", number: 2), token: currentToken))
        XCTAssertEqual(state.version?.id, "v2")
    }

    func testSessionIsCreatedLazilyOnceAndReused() async throws {
        let state = SOPCaptureState()
        state.installCandidates([
            SOPVersionCandidate(id: "v1", sopID: "sop", versionNumber: 1, name: LocalizedText(zhCN: "一", en: "One")),
        ])
        var createCount = 0

        let first = try await state.resolveSession {
            createCount += 1
            return self.sessionFixture(versionID: $0)
        }
        let second = try await state.resolveSession {
            createCount += 1
            return self.sessionFixture(versionID: $0)
        }

        XCTAssertEqual(createCount, 1)
        XCTAssertEqual(first.id, second.id)
    }

    func testConcurrentFirstUploadsShareOneSessionCreation() async throws {
        let state = SOPCaptureState()
        state.installCandidates([
            SOPVersionCandidate(id: "v1", sopID: "sop", versionNumber: 1, name: LocalizedText(zhCN: "一", en: "One")),
        ])
        var createCount = 0
        let create: @MainActor (String) async throws -> PhotoSession = { versionID in
            createCount += 1
            try await Task.sleep(for: .milliseconds(50))
            return self.sessionFixture(versionID: versionID)
        }

        async let first = state.resolveSession(create: create)
        async let second = state.resolveSession(create: create)
        _ = try await (first, second)

        XCTAssertEqual(createCount, 1)
    }

    private func viewFixture(id: String, required: Bool) throws -> SOPView {
        let data = Data(#"{"public_id":"\#(id)","sequence":1,"role":"capture","view_kind":"standard","preset_key":null,"name":{"zh-CN":"视图","en":"View"},"instruction":{"zh-CN":"说明","en":"Instruction"},"required":\#(required),"pose":{"space":"object","camera_position_direction":[0,0,1],"image_up_direction":[1,0,0],"target":[0,0,0]},"composition":{"frame_occupancy":0.85,"aspect_ratio":"1:1","allow_rotation_correction":true,"allow_mirror":false},"reference_images":[]}"#.utf8)
        return try JSONDecoder().decode(SOPView.self, from: data)
    }

    private func summaryFixture(sopID: String, versions: [(String, Int, String)]) throws -> CaptureSOPSummary {
        let versionPayload = versions.map { id, number, status in
            #"{"schema_version":"1.0","public_id":"\#(id)","sop_public_id":"\#(sopID)","version_number":\#(number),"status":"\#(status)","name":{"zh-CN":"规范 \#(number)","en":"SOP \#(number)"},"description":{"zh-CN":"","en":""},"coordinate_system":{"id":"pcs_object_v1","handedness":"right_handed","origin":"bounding_box_center","unit":"normalized","axes":{"x_positive":"object_top","y_positive":"object_left","z_positive":"object_front"}},"published_at":null,"created_at":null,"updated_at":null,"views":[]}"#
        }.joined(separator: ",")
        let data = Data(#"{"public_id":"\#(sopID)","category_id":1,"versions":[\#(versionPayload)],"created_at":null,"updated_at":null}"#.utf8)
        return try JSONDecoder().decode(CaptureSOPSummary.self, from: data)
    }

    private func versionFixture(id: String, number: Int) throws -> SOPVersion {
        try summaryFixture(sopID: "sop", versions: [(id, number, "published")]).versions[0]
    }

    private func sessionFixture(versionID: String) -> PhotoSession {
        PhotoSession(publicID: "session", code: "PS-1", skuID: 1, sopVersionID: versionID, status: "in_progress", createdAt: Date(timeIntervalSince1970: 0))
    }
}
