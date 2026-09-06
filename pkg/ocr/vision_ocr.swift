// macOS 系统 Vision 框架 OCR，由 Go 侧通过 `swift` 调用。
// 用法: swift vision_ocr.swift <图片路径>...
// 输出: JSON 数组，与入参一一对应，每项为 {"text": "..."} 或 {"error": "..."}。
import Foundation
import ImageIO
import Vision

var results: [[String: String]] = []

for path in CommandLine.arguments.dropFirst() {
    let url = URL(fileURLWithPath: path) as CFURL
    guard let source = CGImageSourceCreateWithURL(url, nil),
          let image = CGImageSourceCreateImageAtIndex(source, 0, nil) else {
        results.append(["error": "无法读取图片"])
        continue
    }

    let request = VNRecognizeTextRequest()
    request.recognitionLevel = .accurate
    request.recognitionLanguages = ["zh-Hans", "zh-Hant", "en-US"]
    request.usesLanguageCorrection = true

    do {
        try VNImageRequestHandler(cgImage: image, options: [:]).perform([request])
        let lines = (request.results ?? []).compactMap { $0.topCandidates(1).first?.string }
        results.append(["text": lines.joined(separator: "\n")])
    } catch {
        results.append(["error": error.localizedDescription])
    }
}

let data = try JSONSerialization.data(withJSONObject: results)
FileHandle.standardOutput.write(data)
