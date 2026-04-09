#ifdef __OBJC__
#import <UIKit/UIKit.h>
#else
#ifndef FOUNDATION_EXPORT
#if defined(__cplusplus)
#define FOUNDATION_EXPORT extern "C"
#else
#define FOUNDATION_EXPORT extern
#endif
#endif
#endif

#import "AudioManager.h"
#import "AudioProcessingAdapter.h"
#import "AudioUtils.h"
#import "FlutterBroadcastScreenCapturer.h"
#import "FlutterSocketConnection.h"
#import "FlutterSocketConnectionFrameReader.h"
#import "CameraUtils.h"
#import "FlutterDataPacketCryptor.h"
#import "FlutterRPScreenRecorder.h"
#import "FlutterRTCAudioSink-Interface.h"
#import "FlutterRTCAudioSink.h"
#import "FlutterRTCDataChannel.h"
#import "FlutterRTCDesktopCapturer.h"
#import "FlutterRTCFrameCapturer.h"
#import "FlutterRTCFrameCryptor.h"
#import "FlutterRTCMediaRecorder.h"
#import "FlutterRTCMediaStream.h"
#import "FlutterRTCPeerConnection.h"
#import "FlutterRTCVideoPlatformView.h"
#import "FlutterRTCVideoPlatformViewController.h"
#import "FlutterRTCVideoPlatformViewFactory.h"
#import "FlutterRTCVideoRenderer.h"
#import "FlutterWebRTCPlugin.h"
#import "LocalAudioTrack.h"
#import "LocalTrack.h"
#import "LocalVideoTrack.h"
#import "media_stream_interface.h"
#import "RTCAudioSource+Private.h"
#import "VideoProcessingAdapter.h"

FOUNDATION_EXPORT double flutter_webrtcVersionNumber;
FOUNDATION_EXPORT const unsigned char flutter_webrtcVersionString[];

