import 'package:flutter/foundation.dart';

const _securePageScheme = 'https';

bool get defaultSecureBackendTransport =>
    kIsWeb && Uri.base.scheme.toLowerCase() == _securePageScheme;
